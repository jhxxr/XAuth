package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"xauth/internal/store"
)

func TestSMTPSettingsRoundTrip(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)

	var view struct {
		SMTP struct {
			Enabled     bool   `json:"enabled"`
			Source      string `json:"source"`
			PasswordSet bool   `json:"passwordSet"`
		} `json:"smtp"`
	}
	e.api(ad, http.MethodGet, "/api/admin/settings", nil).json(t, &view)
	if view.SMTP.Enabled || view.SMTP.Source != "environment" || view.SMTP.PasswordSet {
		t.Fatalf("fresh smtp view: %+v", view.SMTP)
	}

	const secret = "s3cret-pass"
	bad := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"smtp": map[string]any{"host": "smtp.example", "port": 587, "user": "mailer", "from": "no-reply@example.com", "tls": true},
	})
	if bad.status != http.StatusBadRequest {
		t.Fatalf("user without password: %d %s", bad.status, bad.body)
	}

	r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"smtp": map[string]any{
			"host": "smtp.example", "port": 587, "user": "mailer", "password": secret,
			"from": "no-reply@example.com", "tls": true,
		},
	})
	if r.status != http.StatusOK {
		t.Fatalf("save smtp: %d %s", r.status, r.body)
	}
	if strings.Contains(string(r.body), secret) {
		t.Fatal("response contains the smtp password")
	}
	var saved struct {
		SMTP struct {
			Host        string `json:"host"`
			PasswordSet bool   `json:"passwordSet"`
			Enabled     bool   `json:"enabled"`
			Source      string `json:"source"`
		} `json:"smtp"`
		Registration struct {
			MailEnabled bool `json:"mailEnabled"`
		} `json:"registration"`
	}
	r.json(t, &saved)
	if !saved.SMTP.Enabled || !saved.SMTP.PasswordSet || saved.SMTP.Source != "database" || saved.SMTP.Host != "smtp.example" {
		t.Fatalf("saved smtp: %+v", saved.SMTP)
	}
	if !saved.Registration.MailEnabled {
		t.Fatal("registration view did not notice the mail server")
	}
	raw, ok, err := e.st.ReadSetting(context.Background(), store.SettingSMTP)
	if err != nil || !ok || !strings.Contains(raw, secret) {
		t.Fatalf("stored smtp: ok=%v err=%v %q", ok, err, raw)
	}

	// A blank password keeps the one already stored.
	r = e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"smtp": map[string]any{
			"host": "smtp.example", "port": 2525, "user": "mailer", "from": "other@example.com", "tls": true,
		},
	})
	if r.status != http.StatusOK {
		t.Fatalf("update without password: %d %s", r.status, r.body)
	}
	raw, _, _ = e.st.ReadSetting(context.Background(), store.SettingSMTP)
	if !strings.Contains(raw, secret) || !strings.Contains(raw, "other@example.com") {
		t.Fatalf("password was not kept: %s", raw)
	}

	ev := e.api(ad, http.MethodGet, "/api/admin/events?group=admin&limit=20", nil)
	if strings.Contains(string(ev.body), secret) {
		t.Fatal("audit log contains the smtp password")
	}

	var session struct {
		MailEnabled bool `json:"mailEnabled"`
	}
	e.api(e.browser(), http.MethodGet, "/api/session", nil).json(t, &session)
	if !session.MailEnabled {
		t.Fatal("session still says mail is off")
	}

	if r = e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"resetSmtp": true}); r.status != http.StatusOK {
		t.Fatalf("reset: %d %s", r.status, r.body)
	}
	r.json(t, &saved)
	if saved.SMTP.Enabled || saved.SMTP.Source != "environment" || saved.SMTP.PasswordSet {
		t.Fatalf("after reset: %+v", saved.SMTP)
	}
}

func TestSMTPOverridesEnvironment(t *testing.T) {
	e := newEnv(t, withSMTP)
	ad := adminBrowser(e)
	var view struct {
		SMTP struct {
			Host    string `json:"host"`
			Enabled bool   `json:"enabled"`
			Source  string `json:"source"`
		} `json:"smtp"`
	}
	e.api(ad, http.MethodGet, "/api/admin/settings", nil).json(t, &view)
	if !view.SMTP.Enabled || view.SMTP.Source != "environment" || view.SMTP.Host != "smtp.test" {
		t.Fatalf("env smtp: %+v", view.SMTP)
	}
	r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"smtp": map[string]any{"host": "other.example", "port": 587, "from": "a@b.c", "tls": false},
	})
	if r.status != http.StatusOK {
		t.Fatalf("override: %d %s", r.status, r.body)
	}
	r.json(t, &view)
	if view.SMTP.Source != "database" || view.SMTP.Host != "other.example" {
		t.Fatalf("overridden: %+v", view.SMTP)
	}
	if r = e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"resetSmtp": true}); r.status != http.StatusOK {
		t.Fatalf("reset: %d %s", r.status, r.body)
	}
	r.json(t, &view)
	if view.SMTP.Source != "environment" || view.SMTP.Host != "smtp.test" || !view.SMTP.Enabled {
		t.Fatalf("restored env: %+v", view.SMTP)
	}
}

func TestDisableSMTPWhileInviteVerifyOn(t *testing.T) {
	e := newEnv(t, withSMTP)
	ad := adminBrowser(e)
	if r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"inviteVerifyEmail": true}); r.status != http.StatusOK {
		t.Fatalf("enable verify: %d %s", r.status, r.body)
	}
	r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"smtp": map[string]any{"host": "", "from": "", "tls": true},
	})
	if r.status != http.StatusConflict {
		t.Fatalf("disable while verification is required: %d %s", r.status, r.body)
	}
	r = e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"inviteVerifyEmail": false,
		"smtp":              map[string]any{"host": "", "from": "", "tls": true},
	})
	if r.status != http.StatusOK {
		t.Fatalf("disable together with verification: %d %s", r.status, r.body)
	}
}

func TestXMailSettingsRoundTrip(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)
	var view struct {
		XMail struct {
			BaseURL string `json:"baseUrl"`
			KeySet  bool   `json:"keySet"`
			Enabled bool   `json:"enabled"`
			Source  string `json:"source"`
		} `json:"xmail"`
	}
	e.api(ad, http.MethodGet, "/api/admin/settings", nil).json(t, &view)
	if view.XMail.Enabled || view.XMail.KeySet || view.XMail.Source != "environment" {
		t.Fatalf("fresh xmail: %+v", view.XMail)
	}
	bad := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"xmail": map[string]any{"baseUrl": "https://mail.example"},
	})
	if bad.status != http.StatusBadRequest {
		t.Fatalf("address without key: %d %s", bad.status, bad.body)
	}
	const key = "sk_live_test_secret"
	r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"xmail": map[string]any{"baseUrl": "https://mail.example/api/v1", "apiKey": key},
	})
	if r.status != http.StatusOK {
		t.Fatalf("save xmail: %d %s", r.status, r.body)
	}
	if strings.Contains(string(r.body), key) {
		t.Fatal("response contains the xmail API key")
	}
	r.json(t, &view)
	if !view.XMail.Enabled || !view.XMail.KeySet || view.XMail.Source != "database" || view.XMail.BaseURL != "https://mail.example" {
		t.Fatalf("saved xmail: %+v", view.XMail)
	}
	raw, ok, err := e.st.ReadSetting(context.Background(), store.SettingXMail)
	if err != nil || !ok || !strings.Contains(raw, key) {
		t.Fatalf("stored xmail: ok=%v err=%v %q", ok, err, raw)
	}
	r = e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{
		"xmail": map[string]any{"baseUrl": "https://other.example"},
	})
	if r.status != http.StatusOK {
		t.Fatalf("update without key: %d %s", r.status, r.body)
	}
	raw, _, _ = e.st.ReadSetting(context.Background(), store.SettingXMail)
	if !strings.Contains(raw, key) || !strings.Contains(raw, "other.example") {
		t.Fatalf("key was not kept: %s", raw)
	}
	ev := e.api(ad, http.MethodGet, "/api/admin/events?group=admin&limit=20", nil)
	if strings.Contains(string(ev.body), key) {
		t.Fatal("audit log contains the xmail API key")
	}
	var status struct {
		Configured bool `json:"configured"`
	}
	e.api(ad, http.MethodGet, "/api/mail/status", nil).json(t, &status)
	if !status.Configured {
		t.Fatal("mail status did not notice xmail")
	}
	if r = e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"resetXmail": true}); r.status != http.StatusOK {
		t.Fatalf("reset: %d %s", r.status, r.body)
	}
	r.json(t, &view)
	if view.XMail.Enabled || view.XMail.Source != "environment" || view.XMail.KeySet {
		t.Fatalf("after reset: %+v", view.XMail)
	}
}
