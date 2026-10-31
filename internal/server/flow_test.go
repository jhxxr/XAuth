package server_test

import (
	"net/http"
	"strings"
	"testing"

	"xauth/internal/store"
)

func audContains(aud any, id string) bool {
	switch v := aud.(type) {
	case string:
		return v == id
	case []any:
		for _, a := range v {
			if a == id {
				return true
			}
		}
	}
	return false
}

func TestCodeFlowRefreshAndReuse(t *testing.T) {
	e := newEnv(t)
	u := e.user("alice@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	b := e.browser()

	code, verifier := e.codeFlow(b, app.ID, u.Email, "correct horse")
	status, tok := e.exchange(app.ID, secret, code, verifier)
	if status != http.StatusOK || tok.AccessToken == "" || tok.IDToken == "" || tok.RefreshToken == "" {
		t.Fatalf("exchange: %d %+v", status, tok)
	}

	claims := e.idClaims(tok.IDToken)
	if claims["iss"] != e.issuer || claims["sub"] != u.ID || claims["nonce"] != "n-s1" || claims["email"] != u.Email {
		t.Fatalf("unexpected ID token claims: %v", claims)
	}
	if !audContains(claims["aud"], app.ID) {
		t.Fatalf("aud %v does not contain %s", claims["aud"], app.ID)
	}

	status, info := e.userinfo(tok.AccessToken)
	if status != http.StatusOK || info["sub"] != u.ID || info["email"] != u.Email || info["name"] != u.Name {
		t.Fatalf("userinfo: %d %v", status, info)
	}

	// Refresh rotates the token; the old one is then rejected and revokes the family.
	status, tok2 := e.refresh(app.ID, secret, tok.RefreshToken)
	if status != http.StatusOK || tok2.RefreshToken == "" || tok2.RefreshToken == tok.RefreshToken {
		t.Fatalf("refresh: %d %+v", status, tok2)
	}
	if status, r := e.refresh(app.ID, secret, tok.RefreshToken); status != http.StatusBadRequest || r.Error != "invalid_grant" {
		t.Fatalf("reused refresh token: %d %+v", status, r)
	}
	if status, _ := e.refresh(app.ID, secret, tok2.RefreshToken); status != http.StatusBadRequest {
		t.Fatalf("successor of a reused token still works: %d", status)
	}
	if status, _ := e.userinfo(tok2.AccessToken); status == http.StatusOK {
		t.Fatal("access token of a revoked family still works")
	}
}

func TestCodeReuseRevokesTokens(t *testing.T) {
	e := newEnv(t)
	u := e.user("bob@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)

	code, verifier := e.codeFlow(e.browser(), app.ID, u.Email, "correct horse")
	status, tok := e.exchange(app.ID, secret, code, verifier)
	if status != http.StatusOK {
		t.Fatalf("exchange: %d", status)
	}
	if status, r := e.exchange(app.ID, secret, code, verifier); status != http.StatusBadRequest || r.Error != "invalid_grant" {
		t.Fatalf("second exchange: %d %+v", status, r)
	}
	if status, _ := e.userinfo(tok.AccessToken); status == http.StatusOK {
		t.Fatal("access token still valid after code reuse")
	}
	if status, _ := e.refresh(app.ID, secret, tok.RefreshToken); status == http.StatusOK {
		t.Fatal("refresh token still valid after code reuse")
	}
}

func TestPKCERequired(t *testing.T) {
	e := newEnv(t)
	app, _ := e.app(store.ClientWeb, rpCallback)
	_, challenge := pkce()

	cases := map[string]authParams{
		"missing": {clientID: app.ID, redirect: rpCallback, scope: "openid", state: "x"},
		"plain":   {clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "plain", scope: "openid", state: "x"},
	}
	for name, p := range cases {
		cb := e.follow(e.browser(), e.authorizeURL(p))
		if !strings.HasPrefix(cb.String(), rpCallback) || cb.Query().Get("error") != "invalid_request" || cb.Query().Get("state") != "x" {
			t.Errorf("%s: expected invalid_request at the app, got %s", name, cb)
		}
	}
}

func TestWrongVerifierRejected(t *testing.T) {
	e := newEnv(t)
	u := e.user("carol@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	code, _ := e.codeFlow(e.browser(), app.ID, u.Email, "correct horse")
	other, _ := pkce()
	if status, r := e.exchange(app.ID, secret, code, other); status != http.StatusBadRequest || r.Error != "invalid_grant" {
		t.Fatalf("wrong verifier: %d %+v", status, r)
	}
}

func TestPublicClientUsesPKCEWithoutSecret(t *testing.T) {
	e := newEnv(t)
	u := e.user("dan@example.com", "correct horse", false)
	app, secret := e.app(store.ClientSPA, rpCallback)
	if secret != "" {
		t.Fatal("SPA client received a secret")
	}
	code, verifier := e.codeFlow(e.browser(), app.ID, u.Email, "correct horse")
	if status, tok := e.exchange(app.ID, "", code, verifier); status != http.StatusOK || tok.AccessToken == "" {
		t.Fatalf("public exchange: %d %+v", status, tok)
	}
}

func TestConfidentialClientNeedsSecret(t *testing.T) {
	e := newEnv(t)
	u := e.user("erin@example.com", "correct horse", false)
	app, _ := e.app(store.ClientWeb, rpCallback)
	code, verifier := e.codeFlow(e.browser(), app.ID, u.Email, "correct horse")
	if status, _ := e.exchange(app.ID, "wrong-secret", code, verifier); status != http.StatusUnauthorized {
		t.Fatalf("wrong secret: %d", status)
	}
}
