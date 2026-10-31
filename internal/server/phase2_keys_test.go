package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	jose "github.com/go-jose/go-jose/v4"

	"xauth/internal/store"
)

func TestKeyRotationRetiresOldTokens(t *testing.T) {
	e := newEnv(t)
	admin := e.user("root@example.com", "correct horse", true)
	u := e.user("sam@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	code, verifier := e.codeFlow(e.browser(), app.ID, u.Email, "correct horse")
	_, old := e.exchange(app.ID, secret, code, verifier)
	oldKID := e.idClaims(old.IDToken)["iss"]
	_ = oldKID

	ad := e.browser()
	login(e, ad, admin.Email, "correct horse")
	var created struct{ Key struct{ ID string } }
	r := e.api(ad, http.MethodPost, "/api/admin/keys", nil)
	if r.status != http.StatusCreated {
		t.Fatalf("rotate: %d %s", r.status, r.body)
	}
	r.json(t, &created)

	code, verifier = e.codeFlow(e.browser(), app.ID, u.Email, "correct horse")
	_, fresh := e.exchange(app.ID, secret, code, verifier)
	tok, err := jose.ParseSigned(fresh.IDToken, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	if tok.Signatures[0].Header.KeyID != created.Key.ID {
		t.Fatalf("new token signed by %s, want %s", tok.Signatures[0].Header.KeyID, created.Key.ID)
	}

	var keys struct {
		Keys []struct {
			ID      string
			Current bool
		}
	}
	e.api(ad, http.MethodGet, "/api/admin/keys", nil).json(t, &keys)
	var oldID string
	for _, k := range keys.Keys {
		if !k.Current {
			oldID = k.ID
		}
	}
	if oldID == "" {
		t.Fatal("old key missing")
	}
	if r := e.api(ad, http.MethodPost, "/api/admin/keys/"+oldID+"/retire", nil); r.status != http.StatusOK {
		t.Fatalf("retire: %d %s", r.status, r.body)
	}
	if status, _ := e.userinfo(old.AccessToken); status == http.StatusOK {
		t.Fatal("token signed by the retired key still works")
	}
	if status, _ := e.userinfo(fresh.AccessToken); status != http.StatusOK {
		t.Fatalf("token signed by the current key stopped working: %d", status)
	}
	if r := e.api(ad, http.MethodPost, "/api/admin/keys/"+created.Key.ID+"/retire", nil); r.status != http.StatusConflict {
		t.Fatalf("retired the last key: %d", r.status)
	}
}

func TestReuseIsAuditedWithoutSecrets(t *testing.T) {
	e := newEnv(t)
	admin := e.user("root@example.com", "correct horse", true)
	u := e.user("tia@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	b := e.browser()
	code, verifier := e.codeFlow(b, app.ID, u.Email, "correct horse")
	_, tok := e.exchange(app.ID, secret, code, verifier)
	e.exchange(app.ID, secret, code, verifier)
	e.refresh(app.ID, secret, tok.RefreshToken)
	_, rotated := e.refresh(app.ID, secret, tok.RefreshToken)
	e.refresh(app.ID, secret, tok.RefreshToken)

	ad := e.browser()
	login(e, ad, admin.Email, "correct horse")
	var page struct {
		Events []struct {
			Type   string
			Detail map[string]any
		}
	}
	r := e.api(ad, http.MethodGet, "/api/admin/events?group=security", nil)
	if r.status != http.StatusOK {
		t.Fatalf("events: %d %s", r.status, r.body)
	}
	r.json(t, &page)
	seen := map[string]bool{}
	for _, event := range page.Events {
		if event.Type == "token.reuse" {
			seen[event.Detail["kind"].(string)] = true
		}
	}
	if !seen["code"] || !seen["refresh"] {
		t.Fatalf("reuse events: %+v", page.Events)
	}
	raw, _ := json.Marshal(page)
	for _, secret := range []string{code, verifier, tok.AccessToken, tok.RefreshToken, rotated.RefreshToken} {
		if secret != "" && strings.Contains(string(raw), secret) {
			t.Fatalf("audit log contains a secret")
		}
	}
}
