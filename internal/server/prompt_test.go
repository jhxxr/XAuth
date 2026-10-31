package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"xauth/internal/store"
)

func TestSSOAndPrompts(t *testing.T) {
	e := newEnv(t)
	u := e.user("fay@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	e.codeFlow(b, app.ID, u.Email, "correct horse")

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

func TestContinueAndDeny(t *testing.T) {
	e := newEnv(t)
	u := e.user("gus@example.com", "correct horse", false)
	app, _ := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	e.codeFlow(b, app.ID, u.Email, "correct horse")

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
