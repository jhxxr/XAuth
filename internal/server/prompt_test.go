package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"xauth/internal/store"
)

func TestSSOAndPrompts(t *testing.T) {
	e := newEnv(t)
	u := e.user("fay@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	e.codeFlowWithScopes(b, app.ID, u.Email, "correct horse", "openid email")

	// A signed-in browser goes straight back to the app.
	verifier, challenge := pkce()
	p := authParams{clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256", scope: "openid email", state: "s2"}
	code := e.codeFrom(e.follow(b, e.authorizeURL(p)), "s2")
	if status, _ := e.exchange(app.ID, secret, code, verifier); status != http.StatusOK {
		t.Fatalf("SSO exchange: %d", status)
	}

	// prompt=login always shows the login page.
	p.prompt, p.state = "login", "s3"
	if got := e.follow(b, e.authorizeURL(p)); got.Path != "/login" {
		t.Fatalf("prompt=login: expected login page, got %s", got)
	}

	// prompt=none works with a session and fails without one.
	p.prompt, p.state = "none", "s4"
	e.codeFrom(e.follow(b, e.authorizeURL(p)), "s4")
	cb := e.follow(e.browser(), e.authorizeURL(p))
	if cb.Query().Get("error") != "login_required" || cb.Query().Get("state") != "s4" {
		t.Fatalf("prompt=none without session: %s", cb)
	}
}

func TestMaxAgeZeroReauthenticationCanContinueToConsent(t *testing.T) {
	e := newEnv(t)
	u := e.user("max-age-zero@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	login(e, b, u.Email, "correct horse")

	verifier, challenge := pkce()
	p := authParams{
		clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256",
		scope: "openid", state: "max-age-zero", maxAge: "0",
	}
	page := e.follow(b, e.authorizeURL(p))
	if page.Path != "/login" {
		t.Fatalf("max_age=0 accepted an existing session without reauthentication: %s", page)
	}
	flowID := page.Query().Get("flow")
	var before struct{ CanContinue bool }
	e.api(b, http.MethodGet, "/api/flow/"+flowID, nil).json(t, &before)
	if before.CanContinue {
		t.Fatal("max_age=0 allowed the old session to continue")
	}

	var signedIn struct {
		RequiresConsent bool
		FlowError       string
	}
	r := e.api(b, http.MethodPost, "/api/login", map[string]string{
		"email": u.Email, "password": "correct horse", "flow": flowID,
	})
	if r.status != http.StatusOK {
		t.Fatalf("reauthenticate: %d %s", r.status, r.body)
	}
	r.json(t, &signedIn)
	if !signedIn.RequiresConsent || signedIn.FlowError != "" {
		t.Fatalf("reauthentication did not lead to the consent step: %+v", signedIn)
	}
	var after struct{ CanContinue, RequiresConsent bool }
	r = e.api(b, http.MethodGet, "/api/flow/"+flowID, nil)
	if r.status != http.StatusOK {
		t.Fatalf("get reauthenticated flow: %d %s", r.status, r.body)
	}
	r.json(t, &after)
	if !after.CanContinue || !after.RequiresConsent {
		t.Fatalf("fresh session could not continue to consent: %+v", after)
	}
	var decision struct{ Redirect string }
	r = e.api(b, http.MethodPost, "/api/flow/"+flowID+"/continue", nil)
	if r.status != http.StatusOK {
		t.Fatalf("approve consent after reauthentication: %d %s", r.status, r.body)
	}
	r.json(t, &decision)
	code := e.codeFrom(e.follow(b, e.abs(decision.Redirect)), p.state)
	status, tokens := e.exchange(app.ID, secret, code, verifier)
	if status != http.StatusOK {
		t.Fatalf("exchange max_age=0 code: %d %+v", status, tokens)
	}
	claims := e.idClaims(tokens.IDToken)
	if methods, ok := claims["amr"].([]any); !ok || len(methods) != 1 || methods[0] != "pwd" {
		t.Fatalf("reauthenticated ID token reports the wrong method: %v", claims["amr"])
	}
	if authTime, ok := claims["auth_time"].(float64); !ok || authTime <= 0 {
		t.Fatalf("reauthenticated ID token has no auth_time: %v", claims["auth_time"])
	}
}

func TestOIDCReusesTheActualAuthenticationMethodFromSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		amr  []string
		want string
	}{{name: "passkey session", amr: []string{"webauthn"}, want: "webauthn"}, {name: "unknown legacy session"}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			u := e.user("session-amr@example.com", "correct horse", false)
			app, secret := e.app(store.ClientWeb, rpCallback)
			raw, _, err := e.st.CreateSession(t.Context(), u.ID, 24*time.Hour, store.SessionMeta{AMR: tc.amr})
			if err != nil {
				t.Fatal(err)
			}
			b := e.browser()
			issuer, err := url.Parse(e.issuer)
			if err != nil {
				t.Fatal(err)
			}
			b.Jar.SetCookies(issuer, []*http.Cookie{{Name: "xauth_session", Value: raw, Path: "/"}})

			verifier, challenge := pkce()
			p := authParams{
				clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256",
				scope: "openid", state: "session-amr",
			}
			page := e.follow(b, e.authorizeURL(p))
			if page.Path != "/login" {
				t.Fatalf("first consent did not bind the existing session: %s", page)
			}
			var flow struct{ CanContinue, RequiresConsent bool }
			r := e.api(b, http.MethodGet, "/api/flow/"+page.Query().Get("flow"), nil)
			if r.status != http.StatusOK {
				t.Fatalf("get authorization flow: %d %s", r.status, r.body)
			}
			r.json(t, &flow)
			if !flow.CanContinue || !flow.RequiresConsent {
				t.Fatalf("unexpected session-bound consent: %+v", flow)
			}
			var decision struct{ Redirect string }
			r = e.api(b, http.MethodPost, "/api/flow/"+page.Query().Get("flow")+"/continue", nil)
			if r.status != http.StatusOK {
				t.Fatalf("approve session-bound consent: %d %s", r.status, r.body)
			}
			r.json(t, &decision)
			code := e.codeFrom(e.follow(b, e.abs(decision.Redirect)), p.state)
			status, tokens := e.exchange(app.ID, secret, code, verifier)
			if status != http.StatusOK {
				t.Fatalf("exchange: %d %+v", status, tokens)
			}
			claims := e.idClaims(tokens.IDToken)
			if tc.want == "" {
				if _, ok := claims["amr"]; ok {
					t.Fatalf("legacy session must not claim a guessed authentication method: %v", claims["amr"])
				}
			} else if methods, ok := claims["amr"].([]any); !ok || len(methods) != 1 || methods[0] != tc.want {
				t.Fatalf("session method was not preserved in amr: %v", claims["amr"])
			}
		})
	}
}

func TestContinueAndDeny(t *testing.T) {
	e := newEnv(t)
	u := e.user("gus@example.com", "correct horse", false)
	app, _ := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	e.codeFlowWithScopes(b, app.ID, u.Email, "correct horse", "openid")

	_, challenge := pkce()
	p := authParams{clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256", scope: "openid", state: "c1", prompt: "select_account"}
	flow := e.follow(b, e.authorizeURL(p)).Query().Get("flow")

	var info struct {
		CanContinue bool
		App         struct{ Name string }
		RedirectTo  string
	}
	e.api(b, http.MethodGet, "/api/flow/"+flow, nil).json(t, &info)
	if !info.CanContinue || info.App.Name != app.Name || info.RedirectTo != "rp.test" {
		t.Fatalf("flow info: %+v", info)
	}
	var out struct{ Redirect string }
	e.api(b, http.MethodPost, "/api/flow/"+flow+"/continue", nil).json(t, &out)
	e.codeFrom(e.follow(b, e.abs(out.Redirect)), "c1")

	// Cancelling sends access_denied back to the app.
	p.state = "c2"
	flow = e.follow(b, e.authorizeURL(p)).Query().Get("flow")
	e.api(b, http.MethodPost, "/api/flow/"+flow+"/deny", nil).json(t, &out)
	cb := e.follow(b, e.abs(out.Redirect))
	if cb.Query().Get("error") != "access_denied" || cb.Query().Get("state") != "c2" {
		t.Fatalf("deny: %s", cb)
	}
	if r := e.api(b, http.MethodGet, "/api/flow/"+flow, nil); r.status != http.StatusGone {
		t.Fatalf("denied flow still pending: %d", r.status)
	}
}

func TestConsentOnFirstLoginScopeExpansionAndRevoke(t *testing.T) {
	e := newEnv(t)
	u := e.user("consent@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	verifier, challenge := pkce()
	p := authParams{clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256", scope: "openid email", state: "first"}
	login := e.follow(b, e.authorizeURL(p))
	if login.Path != "/login" {
		t.Fatalf("first authorization did not open login: %s", login)
	}

	var signedIn struct {
		Redirect        string
		RequiresConsent bool
		FlowError       string
	}
	r := e.api(b, http.MethodPost, "/api/login", map[string]string{
		"email": u.Email, "password": "correct horse", "flow": login.Query().Get("flow"),
	})
	if r.status != http.StatusOK {
		t.Fatalf("login: %d %s", r.status, r.body)
	}
	r.json(t, &signedIn)
	if signedIn.Redirect != "" || !signedIn.RequiresConsent || signedIn.FlowError != "" {
		t.Fatalf("first login bypassed consent: %+v", signedIn)
	}

	var flow struct {
		CanContinue     bool
		RequiresConsent bool
		Scopes          []string
	}
	r = e.api(b, http.MethodGet, "/api/flow/"+login.Query().Get("flow"), nil)
	if r.status != http.StatusOK {
		t.Fatalf("load flow: %d %s", r.status, r.body)
	}
	r.json(t, &flow)
	if !flow.CanContinue || !flow.RequiresConsent || len(flow.Scopes) != 2 {
		t.Fatalf("unexpected consent flow: %+v", flow)
	}
	var decision struct{ Redirect string }
	r = e.api(b, http.MethodPost, "/api/flow/"+login.Query().Get("flow")+"/continue", nil)
	if r.status != http.StatusOK {
		t.Fatalf("approve first consent: %d %s", r.status, r.body)
	}
	r.json(t, &decision)
	code := e.codeFrom(e.follow(b, e.abs(decision.Redirect)), "first")
	if status, _ := e.exchange(app.ID, secret, code, verifier); status != http.StatusOK {
		t.Fatalf("exchange first code: %d", status)
	}

	// The same scopes use SSO, while a newly requested scope needs approval.
	p.state = "same"
	if cb := e.follow(b, e.authorizeURL(p)); !strings.HasPrefix(cb.String(), rpCallback) || cb.Query().Get("code") == "" {
		t.Fatalf("existing consent did not use SSO: %s", cb)
	}
	p.scope, p.state = "openid profile email", "expanded"
	login = e.follow(b, e.authorizeURL(p))
	if login.Path != "/login" {
		t.Fatalf("new scope did not require consent: %s", login)
	}
	r = e.api(b, http.MethodGet, "/api/flow/"+login.Query().Get("flow"), nil)
	if r.status != http.StatusOK {
		t.Fatalf("load expanded flow: %d %s", r.status, r.body)
	}
	r.json(t, &flow)
	if !flow.CanContinue || !flow.RequiresConsent {
		t.Fatalf("expanded scope not marked for consent: %+v", flow)
	}
	if r := e.api(b, http.MethodPost, "/api/flow/"+login.Query().Get("flow")+"/continue", nil); r.status != http.StatusOK {
		t.Fatalf("approve expanded consent: %d %s", r.status, r.body)
	}

	if r := e.api(b, http.MethodDelete, "/api/grants/"+app.ID, nil); r.status != http.StatusOK {
		t.Fatalf("revoke grant: %d %s", r.status, r.body)
	}
	p.scope, p.state = "openid", "revoked"
	login = e.follow(b, e.authorizeURL(p))
	if login.Path != "/login" {
		t.Fatalf("revoked consent did not require a new decision: %s", login)
	}
	r = e.api(b, http.MethodGet, "/api/flow/"+login.Query().Get("flow"), nil)
	if r.status != http.StatusOK {
		t.Fatalf("load revoked flow: %d %s", r.status, r.body)
	}
	r.json(t, &flow)
	if !flow.RequiresConsent {
		t.Fatalf("revoked consent still covered scopes: %+v", flow)
	}
}

func TestExistingSessionStillNeedsFirstAppConsent(t *testing.T) {
	e := newEnv(t)
	u := e.user("existing-session@example.com", "correct horse", false)
	app, _ := e.app(store.ClientSPA, rpCallback)
	b := e.browser()
	login(e, b, u.Email, "correct horse")

	_, challenge := pkce()
	p := authParams{clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256", scope: "openid email", state: "existing-session"}
	page := e.follow(b, e.authorizeURL(p))
	if page.Path != "/login" || page.Query().Get("flow") == "" {
		t.Fatalf("first app authorization skipped confirmation: %s", page)
	}
	var flow struct{ CanContinue, RequiresConsent bool }
	r := e.api(b, http.MethodGet, "/api/flow/"+page.Query().Get("flow"), nil)
	if r.status != http.StatusOK {
		t.Fatalf("load confirmation: %d %s", r.status, r.body)
	}
	r.json(t, &flow)
	if !flow.CanContinue || !flow.RequiresConsent {
		t.Fatalf("existing session did not get consent details: %+v", flow)
	}
	var decision struct{ Redirect string }
	r = e.api(b, http.MethodPost, "/api/flow/"+page.Query().Get("flow")+"/continue", nil)
	if r.status != http.StatusOK {
		t.Fatalf("approve confirmation: %d %s", r.status, r.body)
	}
	r.json(t, &decision)
	if cb := e.follow(b, e.abs(decision.Redirect)); cb.Query().Get("state") != p.state || cb.Query().Get("code") == "" {
		t.Fatalf("approved request did not return to the app: %s", cb)
	}
	p.state = "already-consented"
	if cb := e.follow(b, e.authorizeURL(p)); cb.Query().Get("state") != p.state || cb.Query().Get("code") == "" {
		t.Fatalf("subsequent authorization did not use SSO: %s", cb)
	}
}

func TestConsentDecisionIsBoundToSessionThatSawThePage(t *testing.T) {
	e := newEnv(t)
	first := e.user("consent-owner@example.com", "correct horse", false)
	second := e.user("consent-switch@example.com", "correct horse", false)
	app, _ := e.app(store.ClientSPA, rpCallback)
	b := e.browser()
	login(e, b, first.Email, "correct horse")

	_, challenge := pkce()
	p := authParams{clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256", scope: "openid", state: "session-bound"}
	page := e.follow(b, e.authorizeURL(p))
	if page.Path != "/login" {
		t.Fatalf("first authorization skipped the consent page: %s", page)
	}
	flowID := page.Query().Get("flow")
	var flow struct{ CanContinue, RequiresConsent bool }
	r := e.api(b, http.MethodGet, "/api/flow/"+flowID, nil)
	if r.status != http.StatusOK {
		t.Fatalf("load flow: %d %s", r.status, r.body)
	}
	r.json(t, &flow)
	if !flow.CanContinue || !flow.RequiresConsent {
		t.Fatalf("expected first-account consent page: %+v", flow)
	}

	// A login from another tab changes the browser cookie after the page was rendered.
	login(e, b, second.Email, "correct horse")
	r = e.api(b, http.MethodGet, "/api/flow/"+flowID, nil)
	if r.status != http.StatusOK {
		t.Fatalf("reload flow after account switch: %d %s", r.status, r.body)
	}
	r.json(t, &flow)
	if flow.CanContinue {
		t.Fatal("consent page identity was not bound to its original session")
	}
	if r := e.api(b, http.MethodPost, "/api/flow/"+flowID+"/continue", nil); r.status != http.StatusConflict {
		t.Fatalf("different account approved consent: %d %s", r.status, r.body)
	}
}

func TestConsentCannotMoveToANewSessionOfTheSameUser(t *testing.T) {
	e := newEnv(t)
	u := e.user("same-user-new-session@example.com", "correct horse", false)
	app, _ := e.app(store.ClientSPA, rpCallback)
	b := e.browser()
	login(e, b, u.Email, "correct horse")
	_, challenge := pkce()
	p := authParams{
		clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256",
		scope: "openid", state: "new-session-binding",
	}
	page := e.follow(b, e.authorizeURL(p))
	flowID := page.Query().Get("flow")
	if page.Path != "/login" || flowID == "" {
		t.Fatalf("expected the initial approval page, got %s", page)
	}
	request, err := e.st.AuthRequestByID(t.Context(), flowID)
	if err != nil || request.SessionID == "" {
		t.Fatalf("authorization was not tied to the current session: request=%+v err=%v", request, err)
	}

	// A new login by the same account replaces its session cookie. Its user ID
	// and second-rounded auth_time may match, but it did not see this page.
	login(e, b, u.Email, "correct horse")
	var flow struct{ CanContinue, RequiresConsent bool }
	r := e.api(b, http.MethodGet, "/api/flow/"+flowID, nil)
	if r.status != http.StatusOK {
		t.Fatalf("reload approval after same-user login: %d %s", r.status, r.body)
	}
	r.json(t, &flow)
	if flow.CanContinue {
		t.Fatal("a replacement session of the same account reused a consent decision page")
	}
	if r := e.api(b, http.MethodPost, "/api/flow/"+flowID+"/continue", nil); r.status != http.StatusConflict {
		t.Fatalf("replacement session approved the stale consent page: %d %s", r.status, r.body)
	}
}

func TestPromptConsentAndNoneRespectStoredConsent(t *testing.T) {
	e := newEnv(t)
	u := e.user("forced-consent@example.com", "correct horse", false)
	app, _ := e.app(store.ClientSPA, rpCallback)
	b := e.browser()
	e.codeFlowWithScopes(b, app.ID, u.Email, "correct horse", "openid")

	_, challenge := pkce()
	p := authParams{clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256", scope: "openid", state: "forced", prompt: "consent"}
	login := e.follow(b, e.authorizeURL(p))
	if login.Path != "/login" {
		t.Fatalf("prompt=consent skipped UI: %s", login)
	}
	var flow struct{ CanContinue, RequiresConsent bool }
	e.api(b, http.MethodGet, "/api/flow/"+login.Query().Get("flow"), nil).json(t, &flow)
	if !flow.CanContinue || !flow.RequiresConsent {
		t.Fatalf("prompt=consent did not force approval: %+v", flow)
	}
	var decision struct{ Redirect string }
	e.api(b, http.MethodPost, "/api/flow/"+login.Query().Get("flow")+"/continue", nil).json(t, &decision)
	e.codeFrom(e.follow(b, e.abs(decision.Redirect)), "forced")

	p.scope, p.prompt, p.state = "openid email", "none", "none-new-scope"
	cb := e.follow(b, e.authorizeURL(p))
	if cb.Query().Get("error") != "interaction_required" || cb.Query().Get("state") != p.state {
		t.Fatalf("prompt=none skipped required consent: %s", cb)
	}
	p.scope, p.state = "openid", "none-approved"
	if cb = e.follow(b, e.authorizeURL(p)); cb.Query().Get("code") == "" || cb.Query().Get("state") != p.state {
		t.Fatalf("prompt=none did not use saved consent: %s", cb)
	}
}

func TestConsentFlowRequiresTheAuthenticatedBrowserSession(t *testing.T) {
	e := newEnv(t)
	first := e.user("first-consent@example.com", "correct horse", false)
	second := e.user("second-consent@example.com", "correct horse", false)
	app, _ := e.app(store.ClientSPA, rpCallback)
	b := e.browser()
	_, challenge := pkce()
	p := authParams{clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256", scope: "openid", state: "bound"}
	flowURL := e.follow(b, e.authorizeURL(p))
	e.api(b, http.MethodPost, "/api/login", map[string]string{
		"email": first.Email, "password": "correct horse", "flow": flowURL.Query().Get("flow"),
	})

	// Replacing the browser session with another user cannot reuse the pending approval.
	login(e, b, second.Email, "correct horse")
	var flow struct{ CanContinue bool }
	r := e.api(b, http.MethodGet, "/api/flow/"+flowURL.Query().Get("flow"), nil)
	if r.status != http.StatusOK {
		t.Fatalf("load rebound flow: %d %s", r.status, r.body)
	}
	r.json(t, &flow)
	if flow.CanContinue {
		t.Fatal("different user's browser session can continue the consent flow")
	}
	if r := e.api(b, http.MethodPost, "/api/flow/"+flowURL.Query().Get("flow")+"/continue", nil); r.status != http.StatusConflict {
		t.Fatalf("different user continued flow: %d %s", r.status, r.body)
	}
}

func TestEndSession(t *testing.T) {
	e := newEnv(t)
	u := e.user("hal@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	code, verifier := e.codeFlow(b, app.ID, u.Email, "correct horse")
	_, tok := e.exchange(app.ID, secret, code, verifier)

	// Without id_token_hint the user must confirm; the session survives.
	r := e.get(b, "/end_session")
	if loc := r.location(t); loc.Path != "/logout" {
		t.Fatalf("end_session without hint: %s", loc)
	}
	var sess struct{ User *struct{ ID string } }
	e.api(b, http.MethodGet, "/api/session", nil).json(t, &sess)
	if sess.User == nil {
		t.Fatal("session ended without id_token_hint")
	}

	q := url.Values{"id_token_hint": {tok.IDToken}, "post_logout_redirect_uri": {"http://rp.test/bye"}, "state": {"z"}}
	loc := e.get(b, "/end_session?"+q.Encode()).location(t)
	if !strings.HasPrefix(loc.String(), "http://rp.test/bye") || loc.Query().Get("state") != "z" {
		t.Fatalf("end_session redirect: %s", loc)
	}
	sess.User = nil
	e.api(b, http.MethodGet, "/api/session", nil).json(t, &sess)
	if sess.User != nil {
		t.Fatal("session still active after end_session")
	}
}
