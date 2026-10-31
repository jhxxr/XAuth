package server_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	jose "github.com/go-jose/go-jose/v4"

	"xauth/internal/store"
)

func pkce() (verifier, challenge string) {
	verifier = store.NewToken() + store.NewToken()[:11]
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

type authParams struct {
	clientID, redirect, challenge, method, scope, prompt, state string
}

func (e *env) authorizeURL(p authParams) string {
	q := url.Values{
		"client_id": {p.clientID}, "redirect_uri": {p.redirect}, "response_type": {"code"},
		"scope": {p.scope}, "state": {p.state}, "nonce": {"n-" + p.state},
	}
	if p.challenge != "" {
		q.Set("code_challenge", p.challenge)
		q.Set("code_challenge_method", p.method)
	}
	if p.prompt != "" {
		q.Set("prompt", p.prompt)
	}
	return e.issuer + "/authorize?" + q.Encode()
}

// follow walks XAuth redirects until one leaves the issuer or a page is shown.
func (e *env) follow(c *http.Client, u string) *url.URL {
	e.t.Helper()
	for range 10 {
		r := e.get(c, u)
		if r.status != http.StatusFound && r.status != http.StatusSeeOther {
			e.t.Fatalf("GET %s: expected redirect, got %d: %s", u, r.status, r.body)
		}
		next := r.location(e.t)
		if next.IsAbs() && !strings.HasPrefix(next.String(), e.issuer) {
			return next
		}
		if next.Path == "/login" {
			return next
		}
		u = e.abs(next.String())
	}
	e.t.Fatal("too many redirects")
	return nil
}

// codeFlow signs in through the login API and returns the code and verifier.
func (e *env) codeFlow(c *http.Client, clientID, email, pw string) (code, verifier string) {
	e.t.Helper()
	verifier, challenge := pkce()
	login := e.follow(c, e.authorizeURL(authParams{clientID: clientID, redirect: rpCallback,
		challenge: challenge, method: "S256", scope: "openid profile email offline_access", state: "s1"}))
	if login.Path != "/login" || login.Query().Get("flow") == "" {
		e.t.Fatalf("expected login page, got %s", login)
	}
	var out struct{ Redirect string }
	r := e.api(c, http.MethodPost, "/api/login", map[string]string{
		"email": email, "password": pw, "flow": login.Query().Get("flow")})
	if r.status != http.StatusOK {
		e.t.Fatalf("login: %d %s", r.status, r.body)
	}
	r.json(e.t, &out)
	return e.codeFrom(e.follow(c, e.abs(out.Redirect)), "s1"), verifier
}

func (e *env) codeFrom(cb *url.URL, state string) string {
	e.t.Helper()
	if !strings.HasPrefix(cb.String(), rpCallback) {
		e.t.Fatalf("expected app callback, got %s", cb)
	}
	if got := cb.Query().Get("state"); got != state {
		e.t.Fatalf("state = %q, want %q", got, state)
	}
	code := cb.Query().Get("code")
	if code == "" {
		e.t.Fatalf("no code in %s", cb)
	}
	return code
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
}

func (e *env) token(clientID, secret string, form url.Values) (int, tokenResponse) {
	e.t.Helper()
	if secret == "" {
		form.Set("client_id", clientID)
	}
	req, _ := http.NewRequest(http.MethodPost, e.issuer+"/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if secret != "" {
		req.SetBasicAuth(clientID, secret)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var tr tokenResponse
	_ = json.NewDecoder(res.Body).Decode(&tr)
	return res.StatusCode, tr
}

func (e *env) exchange(clientID, secret, code, verifier string) (int, tokenResponse) {
	return e.token(clientID, secret, url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {rpCallback}, "code_verifier": {verifier}})
}

func (e *env) refresh(clientID, secret, rt string) (int, tokenResponse) {
	return e.token(clientID, secret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}})
}

func (e *env) userinfo(at string) (int, map[string]any) {
	e.t.Helper()
	r := do(e.t, http.DefaultClient, http.MethodGet, e.issuer+"/userinfo", nil, map[string]string{"Authorization": "Bearer " + at})
	var m map[string]any
	_ = json.Unmarshal(r.body, &m)
	return r.status, m
}

// idClaims verifies the ID token signature against the JWKS endpoint.
func (e *env) idClaims(raw string) map[string]any {
	e.t.Helper()
	var set jose.JSONWebKeySet
	do(e.t, http.DefaultClient, http.MethodGet, e.issuer+"/keys", nil, nil).json(e.t, &set)
	tok, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		e.t.Fatal(err)
	}
	keys := set.Key(tok.Signatures[0].Header.KeyID)
	if len(keys) == 0 {
		e.t.Fatal("ID token key not in JWKS")
	}
	payload, err := tok.Verify(keys[0])
	if err != nil {
		e.t.Fatalf("ID token signature: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		e.t.Fatal(err)
	}
	return claims
}
