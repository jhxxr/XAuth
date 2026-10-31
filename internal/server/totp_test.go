package server_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTotpVault(t *testing.T) {
	e := newEnv(t)
	e.user("admin@example.com", "admin-pass", true)
	bob := e.user("bob@example.com", "bob-pass", false)
	e.user("cara@example.com", "cara-pass", false)

	admin := e.browser()
	e.api(admin, http.MethodPost, "/api/login", map[string]string{"email": "admin@example.com", "password": "admin-pass"})
	anon := e.browser()
	if r := e.api(anon, http.MethodGet, "/api/admin/totp", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("anonymous admin list: %d", r.status)
	}
	bobc := e.browser()
	e.api(bobc, http.MethodPost, "/api/login", map[string]string{"email": "bob@example.com", "password": "bob-pass"})
	if r := e.api(bobc, http.MethodGet, "/api/admin/totp", nil); r.status != http.StatusForbidden {
		t.Fatalf("user admin list: %d", r.status)
	}

	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	var created struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	r := e.api(admin, http.MethodPost, "/api/admin/totp", map[string]any{
		"input":   "otpauth://totp/GitHub:ops?secret=" + secret + "&issuer=GitHub&digits=8",
		"userIds": []string{bob.ID},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.status, r.body)
	}
	r.json(t, &created)
	if strings.Contains(string(r.body), secret) {
		t.Fatal("create response contains the secret")
	}

	var mine struct {
		Entries []struct {
			ID        string `json:"id"`
			Code      string `json:"code"`
			Remaining int    `json:"remaining"`
			Shared    bool   `json:"shared"`
		} `json:"entries"`
	}
	e.st.SetClock(func() time.Time { return time.Unix(59, 0) })
	r = e.get(bobc, "/api/totp")
	r.json(t, &mine)
	if len(mine.Entries) != 1 || mine.Entries[0].Code != "94287082" || !mine.Entries[0].Shared {
		t.Fatalf("bob codes: %s", r.body)
	}
	if strings.Contains(string(r.body), secret) {
		t.Fatal("code response contains the secret")
	}

	cara := e.browser()
	e.api(cara, http.MethodPost, "/api/login", map[string]string{"email": "cara@example.com", "password": "cara-pass"})
	r = e.get(cara, "/api/totp")
	r.json(t, &mine)
	if len(mine.Entries) != 0 {
		t.Fatalf("cara sees a shared entry: %s", r.body)
	}

	personal := "otpauth://totp/Bank:cara?secret=GEZDGNBVGY3TQOJQ&issuer=Bank"
	r = e.api(cara, http.MethodPost, "/api/totp", map[string]string{"input": personal})
	if r.status != http.StatusCreated {
		t.Fatalf("personal create: %d %s", r.status, r.body)
	}
	var own struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	r.json(t, &own)
	if r := e.api(admin, http.MethodPatch, "/api/admin/totp/"+own.Entry.ID, map[string]any{"name": "stolen"}); r.status != http.StatusNotFound {
		t.Fatalf("admin edited a personal entry: %d %s", r.status, r.body)
	}
	if r := e.get(bobc, "/api/totp"); strings.Contains(string(r.body), own.Entry.ID) {
		t.Fatal("bob sees cara's personal entry")
	}

	if r := e.api(admin, http.MethodPut, "/api/admin/totp/"+created.Entry.ID+"/grants", map[string]any{"userIds": []string{}}); r.status != http.StatusOK {
		t.Fatalf("unassign: %d %s", r.status, r.body)
	}
	r = e.get(bobc, "/api/totp")
	r.json(t, &mine)
	if len(mine.Entries) != 0 {
		t.Fatalf("grant survived: %s", r.body)
	}

	events := e.get(admin, "/api/admin/events?limit=20")
	if strings.Contains(string(events.body), secret) {
		t.Fatal("audit log contains the secret")
	}
}
