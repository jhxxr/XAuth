package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// adminBrowser creates and signs in an admin in e.
func adminBrowser(e *env) *http.Client {
	e.t.Helper()
	e.user("root@example.com", "correct horse", true)
	b := e.browser()
	login(e, b, "root@example.com", "correct horse")
	return b
}

func TestRegistrationDefaultsToInviteOnly(t *testing.T) {
	e := newEnv(t)
	var sess struct {
		RegistrationMode  string `json:"registrationMode"`
		SignupEnabled     bool   `json:"signupEnabled"`
		InviteVerifyEmail bool   `json:"inviteVerifyEmail"`
	}
	e.api(e.browser(), http.MethodGet, "/api/session", nil).json(t, &sess)
	if sess.RegistrationMode != "invite" || sess.SignupEnabled || sess.InviteVerifyEmail {
		t.Fatalf("unexpected default policy: %+v", sess)
	}
	r := e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "new@example.com", "password": "correct horse",
	})
	if r.status != http.StatusForbidden {
		t.Fatalf("open registration without invite: %d %s", r.status, r.body)
	}
}

func TestInviteRegistrationFlow(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)

	var created struct {
		Invite struct {
			ID     string `json:"id"`
			Email  string `json:"email"`
			Status string `json:"status"`
		} `json:"invite"`
		Code string `json:"code"`
		Link string `json:"link"`
	}
	r := e.api(ad, http.MethodPost, "/api/admin/invites", map[string]any{
		"email": "new@example.com", "note": "welcome", "expiresInDays": 2,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create invite: %d %s", r.status, r.body)
	}
	r.json(t, &created)
	if created.Code == "" || !strings.Contains(created.Link, "/register?invite="+created.Code) {
		t.Fatalf("unexpected invite: %+v", created)
	}
	if created.Invite.Email != "new@example.com" || created.Invite.Status != "pending" {
		t.Fatalf("unexpected invite row: %+v", created.Invite)
	}

	// The plaintext code must never be stored or listed.
	if list := e.api(ad, http.MethodGet, "/api/admin/invites", nil); strings.Contains(string(list.body), created.Code) {
		t.Fatal("invite list leaks the code")
	}

	var info struct {
		Invite struct {
			Email     string `json:"email"`
			ExpiresAt string `json:"expiresAt"`
		} `json:"invite"`
	}
	e.get(http.DefaultClient, "/api/invite?code="+url.QueryEscape(created.Code)).json(t, &info)
	if info.Invite.Email != "new@example.com" || info.Invite.ExpiresAt == "" {
		t.Fatalf("invite info: %+v", info.Invite)
	}

	// The invite is bound to one address.
	r = e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "other@example.com", "password": "correct horse", "invite": created.Code,
	})
	if r.status != http.StatusForbidden {
		t.Fatalf("wrong address accepted: %d %s", r.status, r.body)
	}

	// Case and separators do not matter.
	messy := strings.ToLower(strings.ReplaceAll(created.Code, "-", " "))
	var reg struct {
		User struct {
			Email         string `json:"email"`
			EmailVerified bool   `json:"emailVerified"`
		} `json:"user"`
	}
	r = e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "new@example.com", "password": "correct horse", "name": "New", "invite": messy,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("invite registration: %d %s", r.status, r.body)
	}
	r.json(t, &reg)
	if reg.User.Email != "new@example.com" || !reg.User.EmailVerified {
		t.Fatalf("invited user should skip email verification: %+v", reg.User)
	}

	// A code is single use.
	r = e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "another@example.com", "password": "correct horse", "invite": created.Code,
	})
	if r.status != http.StatusGone {
		t.Fatalf("reused invite: %d %s", r.status, r.body)
	}
	if r := e.get(http.DefaultClient, "/api/invite?code="+url.QueryEscape(created.Code)); r.status != http.StatusGone {
		t.Fatalf("used invite still advertised: %d", r.status)
	}

	// A revoked invite cannot be redeemed either.
	var second struct {
		Code string `json:"code"`
	}
	e.api(ad, http.MethodPost, "/api/admin/invites", map[string]any{}).json(t, &second)
	if r := e.api(ad, http.MethodPost, "/api/admin/invites/"+created.Invite.ID+"/revoke", nil); r.status != http.StatusConflict {
		t.Fatalf("revoke a used invite: %d %s", r.status, r.body)
	}
}

func TestRevokedInviteCannotRegister(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)
	var created struct {
		Invite struct {
			ID string `json:"id"`
		} `json:"invite"`
		Code string `json:"code"`
	}
	e.api(ad, http.MethodPost, "/api/admin/invites", map[string]any{}).json(t, &created)
	if r := e.api(ad, http.MethodPost, "/api/admin/invites/"+created.Invite.ID+"/revoke", nil); r.status != http.StatusOK {
		t.Fatalf("revoke: %d %s", r.status, r.body)
	}
	r := e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "new@example.com", "password": "correct horse", "invite": created.Code,
	})
	if r.status != http.StatusGone {
		t.Fatalf("revoked invite accepted: %d %s", r.status, r.body)
	}
}

func TestRegistrationModeSettings(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)

	var settings struct {
		Registration struct {
			Mode string `json:"mode"`
		} `json:"registration"`
	}
	e.api(ad, http.MethodGet, "/api/admin/settings", nil).json(t, &settings)
	if settings.Registration.Mode != "invite" {
		t.Fatalf("default mode %q", settings.Registration.Mode)
	}
	if r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"mode": "bogus"}); r.status != http.StatusBadRequest {
		t.Fatalf("invalid mode accepted: %d", r.status)
	}

	if r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"mode": "open"}); r.status != http.StatusOK {
		t.Fatalf("open registration: %d %s", r.status, r.body)
	}
	r := e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "open@example.com", "password": "correct horse",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("open registration: %d %s", r.status, r.body)
	}

	if r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"mode": "closed"}); r.status != http.StatusOK {
		t.Fatalf("close registration: %d %s", r.status, r.body)
	}
	if r := e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "closed@example.com", "password": "correct horse",
	}); r.status != http.StatusForbidden {
		t.Fatalf("closed registration accepted: %d %s", r.status, r.body)
	}

	// Even a valid invite cannot open an account while registration is closed.
	var created struct {
		Code string `json:"code"`
	}
	e.api(ad, http.MethodPost, "/api/admin/invites", map[string]any{}).json(t, &created)
	if r := e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "closed@example.com", "password": "correct horse", "invite": created.Code,
	}); r.status != http.StatusForbidden {
		t.Fatalf("invite accepted while closed: %d %s", r.status, r.body)
	}
}

func TestInviteVerifyEmailSetting(t *testing.T) {
	e := newEnv(t, withSMTP)
	capture := &captureMailer{}
	e.setMailer(capture)
	ad := adminBrowser(e)

	if r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"inviteVerifyEmail": true}); r.status != http.StatusOK {
		t.Fatalf("enable verification: %d %s", r.status, r.body)
	}
	var created struct {
		Code string `json:"code"`
	}
	e.api(ad, http.MethodPost, "/api/admin/invites", map[string]any{"email": "new@example.com"}).json(t, &created)

	var reg struct {
		User struct {
			EmailVerified bool `json:"emailVerified"`
		} `json:"user"`
	}
	r := e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "new@example.com", "password": "correct horse", "invite": created.Code,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("register: %d %s", r.status, r.body)
	}
	r.json(t, &reg)
	if reg.User.EmailVerified {
		t.Fatal("invited user verified despite the setting")
	}
	msg := capture.last(t)
	if msg.To != "new@example.com" || !strings.Contains(msg.Text, "/verify?token=") {
		t.Fatalf("verification mail: %+v", msg)
	}
}

func TestInviteEmailDelivery(t *testing.T) {
	e := newEnv(t, withSMTP)
	capture := &captureMailer{}
	e.setMailer(capture)
	ad := adminBrowser(e)

	var created struct {
		Code    string `json:"code"`
		Emailed bool   `json:"emailed"`
	}
	r := e.api(ad, http.MethodPost, "/api/admin/invites", map[string]any{
		"email": "new@example.com", "note": "see you", "sendEmail": true,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create invite: %d %s", r.status, r.body)
	}
	r.json(t, &created)
	if !created.Emailed {
		t.Fatal("invite mail was not delivered")
	}
	msg := capture.last(t)
	if msg.To != "new@example.com" || !strings.Contains(msg.Text, created.Code) ||
		!strings.Contains(msg.Text, "/register?invite=") || !strings.Contains(msg.Text, "see you") {
		t.Fatalf("unexpected invite mail: %+v", msg)
	}
	// The deadline is labelled UTC, so the clock in the mail must be UTC too.
	deadline := time.Now().UTC().Add(7 * 24 * time.Hour)
	if !strings.Contains(msg.Text, deadline.Format("2006-01-02 15:04")) &&
		!strings.Contains(msg.Text, deadline.Add(-time.Minute).Format("2006-01-02 15:04")) {
		t.Fatalf("expiry is not UTC: %s", msg.Text)
	}
}

func TestUnboundInviteDoesNotVerifyEmail(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)
	var created struct {
		Code string `json:"code"`
	}
	e.api(ad, http.MethodPost, "/api/admin/invites", map[string]any{}).json(t, &created)
	var reg struct {
		User struct {
			EmailVerified bool `json:"emailVerified"`
		} `json:"user"`
	}
	r := e.api(e.browser(), http.MethodPost, "/api/register", map[string]string{
		"email": "new@example.com", "password": "correct horse", "invite": created.Code,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("register: %d %s", r.status, r.body)
	}
	r.json(t, &reg)
	if reg.User.EmailVerified {
		t.Fatal("an invite that is not bound to an address marked that address verified")
	}
}

func TestInviteGuessesAreRateLimited(t *testing.T) {
	e := newEnv(t)
	body := map[string]string{
		"email": "new@example.com", "password": "correct horse", "invite": "AAAA-BBBB-CCCC",
	}
	for i := 0; i < 30; i++ {
		if r := e.api(e.browser(), http.MethodPost, "/api/register", body); r.status != http.StatusGone {
			t.Fatalf("guess %d: %d %s", i, r.status, r.body)
		}
	}
	if r := e.api(e.browser(), http.MethodPost, "/api/register", body); r.status != http.StatusTooManyRequests {
		t.Fatalf("guess past the limit: %d %s", r.status, r.body)
	}
}

func TestInviteValidation(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)
	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{"bad email", map[string]any{"email": "nope"}, http.StatusBadRequest},
		{"expiry too long", map[string]any{"expiresInDays": 91}, http.StatusBadRequest},
		{"expiry negative", map[string]any{"expiresInDays": -1}, http.StatusBadRequest},
		{"long note", map[string]any{"note": strings.Repeat("a", 201)}, http.StatusBadRequest},
		// Zero selects the seven-day default.
		{"default expiry", map[string]any{}, http.StatusCreated},
	}
	for _, tc := range tests {
		if r := e.api(ad, http.MethodPost, "/api/admin/invites", tc.body); r.status != tc.want {
			t.Fatalf("%s: %d %s", tc.name, r.status, r.body)
		}
	}
}

func TestInviteVerifyEmailRequiresSMTP(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)
	r := e.api(ad, http.MethodPatch, "/api/admin/settings", map[string]any{"inviteVerifyEmail": true})
	if r.status != http.StatusConflict {
		t.Fatalf("enable verification without SMTP: %d %s", r.status, r.body)
	}
}

func TestInviteEmailDeliveryRequiresSMTP(t *testing.T) {
	e := newEnv(t)
	ad := adminBrowser(e)
	r := e.api(ad, http.MethodPost, "/api/admin/invites", map[string]any{
		"email": "new@example.com", "sendEmail": true,
	})
	if r.status != http.StatusConflict {
		t.Fatalf("mail without SMTP: %d %s", r.status, r.body)
	}
}
