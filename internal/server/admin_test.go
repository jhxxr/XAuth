package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"xauth/internal/store"
)

func login(e *env, c *http.Client, email, pw string) {
	e.t.Helper()
	if r := e.api(c, http.MethodPost, "/api/login", map[string]string{"email": email, "password": pw}); r.status != http.StatusOK {
		e.t.Fatalf("login %s: %d %s", email, r.status, r.body)
	}
}

func TestDiscoveryAdvertisesCodeFlowOnly(t *testing.T) {
	e := newEnv(t)
	var d struct {
		Issuer        string   `json:"issuer"`
		ResponseTypes []string `json:"response_types_supported"`
		GrantTypes    []string `json:"grant_types_supported"`
		PKCE          []string `json:"code_challenge_methods_supported"`
		Device        string   `json:"device_authorization_endpoint"`
		JWKS          string   `json:"jwks_uri"`
	}
	r := e.get(http.DefaultClient, "/.well-known/openid-configuration")
	r.json(t, &d)
	if d.Issuer != e.issuer || d.JWKS != e.issuer+"/keys" || d.Device != "" {
		t.Fatalf("discovery endpoints: %+v", d)
	}
	if strings.Join(d.ResponseTypes, ",") != "code" || strings.Join(d.PKCE, ",") != "S256" ||
		strings.Join(d.GrantTypes, ",") != "authorization_code,refresh_token" {
		t.Fatalf("discovery advertises unsupported flows: %+v", d)
	}
	if r.header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("discovery is not readable cross-origin")
	}
}

func TestCrossSiteRequestsBlocked(t *testing.T) {
	e := newEnv(t)
	e.user("ivy@example.com", "correct horse", false)
	body := `{"email":"ivy@example.com","password":"correct horse"}`
	cases := map[string]map[string]string{
		"cross-site fetch": {"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"},
		"foreign origin":   {"Content-Type": "application/json", "Origin": "https://evil.example"},
		"form post":        {"Content-Type": "application/x-www-form-urlencoded"},
	}
	for name, hdr := range cases {
		r := do(t, e.browser(), http.MethodPost, e.issuer+"/api/login", strings.NewReader(body), hdr)
		if r.status != http.StatusForbidden && r.status != http.StatusUnsupportedMediaType {
			t.Errorf("%s: got %d", name, r.status)
		}
	}
}

func TestAdminAPI(t *testing.T) {
	e := newEnv(t)
	admin := e.user("root@example.com", "correct horse", true)
	e.user("joe@example.com", "correct horse", false)

	anon := e.browser()
	if r := e.api(anon, http.MethodGet, "/api/admin/users", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", r.status)
	}
	user := e.browser()
	login(e, user, "joe@example.com", "correct horse")
	if r := e.api(user, http.MethodGet, "/api/admin/apps", nil); r.status != http.StatusForbidden {
		t.Fatalf("non-admin: %d", r.status)
	}

	ad := e.browser()
	login(e, ad, admin.Email, "correct horse")
	if r := e.api(ad, http.MethodPatch, "/api/admin/users/"+admin.ID, map[string]bool{"disabled": true}); r.status != http.StatusBadRequest {
		t.Fatalf("admin disabled self: %d", r.status)
	}

	bad := map[string]any{"name": "SPA", "type": "spa", "redirectUris": []string{"javascript:alert(1)"}}
	if r := e.api(ad, http.MethodPost, "/api/admin/apps", bad); r.status != http.StatusBadRequest {
		t.Fatalf("bad redirect accepted: %d", r.status)
	}
	var created struct {
		App    struct{ ID string }
		Secret string
	}
	good := map[string]any{"name": "Grafana", "type": "web", "redirectUris": []string{rpCallback}}
	r := e.api(ad, http.MethodPost, "/api/admin/apps", good)
	if r.status != http.StatusCreated {
		t.Fatalf("create app: %d %s", r.status, r.body)
	}
	r.json(t, &created)
	if created.Secret == "" {
		t.Fatal("web app created without a secret")
	}
	if strings.Contains(string(e.api(ad, http.MethodGet, "/api/admin/apps", nil).body), created.Secret) {
		t.Fatal("app list leaks the client secret")
	}
}

func TestDisablingUserRevokesTokens(t *testing.T) {
	e := newEnv(t)
	admin := e.user("root@example.com", "correct horse", true)
	u := e.user("kim@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	code, verifier := e.codeFlow(b, app.ID, u.Email, "correct horse")
	_, tok := e.exchange(app.ID, secret, code, verifier)

	ad := e.browser()
	login(e, ad, admin.Email, "correct horse")
	if r := e.api(ad, http.MethodPatch, "/api/admin/users/"+u.ID, map[string]bool{"disabled": true}); r.status != http.StatusOK {
		t.Fatalf("disable: %d %s", r.status, r.body)
	}
	if status, _ := e.userinfo(tok.AccessToken); status == http.StatusOK {
		t.Fatal("disabled user's access token still works")
	}
	if status, _ := e.refresh(app.ID, secret, tok.RefreshToken); status == http.StatusOK {
		t.Fatal("disabled user's refresh token still works")
	}
	var sess struct{ User *struct{ ID string } }
	e.api(b, http.MethodGet, "/api/session", nil).json(t, &sess)
	if sess.User != nil {
		t.Fatal("disabled user's browser session still active")
	}
}

func TestIntrospectionOnlyForOwnTokens(t *testing.T) {
	e := newEnv(t)
	u := e.user("lee@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	other, otherSecret := e.app(store.ClientWeb, rpCallback)
	code, verifier := e.codeFlow(e.browser(), app.ID, u.Email, "correct horse")
	_, tok := e.exchange(app.ID, secret, code, verifier)

	active := func(id, sec string) bool {
		req, _ := http.NewRequest(http.MethodPost, e.issuer+"/oauth/introspect",
			strings.NewReader(url.Values{"token": {tok.AccessToken}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(id, sec)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out struct{ Active bool }
		_ = json.NewDecoder(res.Body).Decode(&out)
		return out.Active
	}
	if !active(app.ID, secret) {
		t.Fatal("token inactive for its own client")
	}
	if active(other.ID, otherSecret) {
		t.Fatal("another client can introspect the token")
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.user("max@example.com", "correct horse", false)
	b := e.browser()
	for i := 0; i < 10; i++ {
		e.api(b, http.MethodPost, "/api/login", map[string]string{"email": "max@example.com", "password": "wrong"})
	}
	r := e.api(b, http.MethodPost, "/api/login", map[string]string{"email": "max@example.com", "password": "correct horse"})
	if r.status != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after repeated failures, got %d", r.status)
	}
}
