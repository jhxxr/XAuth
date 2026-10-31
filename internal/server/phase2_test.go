package server_test

import (
	"net/http"
	"testing"

	"xauth/internal/store"
)

func TestSessionsAndRevokeOthers(t *testing.T) {
	e := newEnv(t)
	u := e.user("nina@example.com", "correct horse", false)
	first, second := e.browser(), e.browser()
	login(e, first, u.Email, "correct horse")
	login(e, second, u.Email, "correct horse")

	var listed struct {
		Sessions []struct {
			ID      string
			Current bool
		}
	}
	r := e.api(second, http.MethodGet, "/api/sessions", nil)
	if r.status != http.StatusOK {
		t.Fatalf("list sessions: %d %s", r.status, r.body)
	}
	r.json(t, &listed)
	if len(listed.Sessions) != 2 {
		t.Fatalf("sessions: %+v", listed.Sessions)
	}
	var current string
	for _, row := range listed.Sessions {
		if row.Current {
			current = row.ID
		}
	}
	if current == "" {
		t.Fatal("current session not marked")
	}

	if r := e.api(second, http.MethodPost, "/api/sessions/revoke-others", nil); r.status != http.StatusOK {
		t.Fatalf("revoke others: %d %s", r.status, r.body)
	}
	e.api(second, http.MethodGet, "/api/sessions", nil).json(t, &listed)
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != current {
		t.Fatalf("current session lost: %+v", listed.Sessions)
	}
	var gone struct{ User *struct{ ID string } }
	e.api(first, http.MethodGet, "/api/session", nil).json(t, &gone)
	if gone.User != nil {
		t.Fatal("revoked browser still signed in")
	}
}

func TestAppAccessAdminsOnly(t *testing.T) {
	e := newEnv(t)
	admin := e.user("root@example.com", "correct horse", true)
	user := e.user("olga@example.com", "correct horse", false)
	app, _ := e.app(store.ClientWeb, rpCallback)
	ad := e.browser()
	login(e, ad, admin.Email, "correct horse")
	if r := e.api(ad, http.MethodPatch, "/api/admin/apps/"+app.ID, map[string]string{"access": "admins"}); r.status != http.StatusOK {
		t.Fatalf("restrict app: %d %s", r.status, r.body)
	}

	_, challenge := pkce()
	p := authParams{clientID: app.ID, redirect: rpCallback, challenge: challenge, method: "S256", scope: "openid", state: "a1", prompt: "select_account"}
	member := e.browser()
	login(e, member, user.Email, "correct horse")
	flow := e.follow(member, e.authorizeURL(p)).Query().Get("flow")
	if r := e.api(member, http.MethodPost, "/api/flow/"+flow+"/continue", nil); r.status != http.StatusForbidden {
		t.Fatalf("member continued: %d %s", r.status, r.body)
	}

	p.state = "a2"
	flow = e.follow(ad, e.authorizeURL(p)).Query().Get("flow")
	var out struct{ Redirect string }
	r := e.api(ad, http.MethodPost, "/api/flow/"+flow+"/continue", nil)
	if r.status != http.StatusOK {
		t.Fatalf("admin continue: %d %s", r.status, r.body)
	}
	r.json(t, &out)
	if out.Redirect == "" {
		t.Fatalf("admin got no redirect: %s", r.body)
	}
}

func TestNarrowingAccessRevokesTokens(t *testing.T) {
	e := newEnv(t)
	admin := e.user("root@example.com", "correct horse", true)
	u := e.user("pia@example.com", "correct horse", false)
	app, secret := e.app(store.ClientWeb, rpCallback)
	code, verifier := e.codeFlow(e.browser(), app.ID, u.Email, "correct horse")
	_, tok := e.exchange(app.ID, secret, code, verifier)

	ad := e.browser()
	login(e, ad, admin.Email, "correct horse")
	body := map[string]any{"access": "selected", "allowedUserIds": []string{admin.ID}}
	if r := e.api(ad, http.MethodPatch, "/api/admin/apps/"+app.ID, body); r.status != http.StatusOK {
		t.Fatalf("narrow access: %d %s", r.status, r.body)
	}
	if status, _ := e.userinfo(tok.AccessToken); status == http.StatusOK {
		t.Fatal("access token still works after the user lost access")
	}
	if status, _ := e.refresh(app.ID, secret, tok.RefreshToken); status == http.StatusOK {
		t.Fatal("refresh token still works after the user lost access")
	}
}

func TestForceSignOutKeepsAdmin(t *testing.T) {
	e := newEnv(t)
	admin := e.user("root@example.com", "correct horse", true)
	u := e.user("quinn@example.com", "correct horse", false)
	member := e.browser()
	login(e, member, u.Email, "correct horse")
	ad := e.browser()
	login(e, ad, admin.Email, "correct horse")

	if r := e.api(ad, http.MethodPost, "/api/admin/users/"+admin.ID+"/signout", nil); r.status != http.StatusBadRequest {
		t.Fatalf("admin signed out self: %d", r.status)
	}
	if r := e.api(ad, http.MethodPost, "/api/admin/users/"+u.ID+"/signout", nil); r.status != http.StatusOK {
		t.Fatalf("force signout: %d %s", r.status, r.body)
	}
	var sess struct{ User *struct{ ID string } }
	e.api(member, http.MethodGet, "/api/session", nil).json(t, &sess)
	if sess.User != nil {
		t.Fatal("target user still signed in")
	}
	e.api(ad, http.MethodGet, "/api/session", nil).json(t, &sess)
	if sess.User == nil || sess.User.ID != admin.ID {
		t.Fatal("admin session was ended")
	}
	if r := e.api(member, http.MethodGet, "/api/admin/events", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("signed-out user reached admin events: %d", r.status)
	}
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	e := newEnv(t)
	e.user("root@example.com", "correct horse", true)
	u := e.user("ria@example.com", "correct horse", false)
	anon := e.browser()
	for _, path := range []string{"/api/admin/events", "/api/admin/keys"} {
		if r := e.api(anon, http.MethodGet, path, nil); r.status != http.StatusUnauthorized {
			t.Errorf("anonymous %s: %d", path, r.status)
		}
	}
	member := e.browser()
	login(e, member, u.Email, "correct horse")
	for _, path := range []string{"/api/admin/events", "/api/admin/keys"} {
		if r := e.api(member, http.MethodGet, path, nil); r.status != http.StatusForbidden {
			t.Errorf("member %s: %d", path, r.status)
		}
	}
}
