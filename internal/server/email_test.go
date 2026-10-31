package server_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"xauth/internal/config"
	"xauth/internal/mailer"
)

type captureMailer struct {
	mu   sync.Mutex
	sent []mailer.Message
}

func (c *captureMailer) Send(m mailer.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return nil
}

func (c *captureMailer) last(t *testing.T) mailer.Message {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		t.Fatal("no mail was sent")
	}
	return c.sent[len(c.sent)-1]
}

func withSMTP(c *config.Config) {
	c.SMTP.Host = "smtp.test"
	c.SMTP.From = "xauth@example.com"
}

func TestEmailVerificationFlow(t *testing.T) {
	e := newEnv(t, withSMTP)
	capture := &captureMailer{}
	e.setMailer(capture)
	e.user("alice@example.com", "alice-pass", false)

	c := e.browser()
	e.api(c, http.MethodPost, "/api/login", map[string]string{"email": "alice@example.com", "password": "alice-pass"})
	if r := e.api(c, http.MethodPost, "/api/account/verify-email", nil); r.status != http.StatusOK {
		t.Fatalf("request verify: %d %s", r.status, r.body)
	}
	msg := capture.last(t)
	if msg.To != "alice@example.com" || !strings.Contains(msg.Text, "/verify?token=") {
		t.Fatalf("unexpected mail: %+v", msg)
	}
	token := tokenFrom(msg.Text)

	if r := e.api(c, http.MethodPost, "/api/verify", map[string]string{"token": token}); r.status != http.StatusOK {
		t.Fatalf("verify: %d %s", r.status, r.body)
	}
	var s struct {
		User struct {
			EmailVerified bool `json:"emailVerified"`
		} `json:"user"`
	}
	e.get(c, "/api/session").json(t, &s)
	if !s.User.EmailVerified {
		t.Fatal("email_verified stayed false")
	}
	if r := e.api(c, http.MethodPost, "/api/verify", map[string]string{"token": token}); r.status != http.StatusGone {
		t.Fatalf("reused link: %d %s", r.status, r.body)
	}
}

func TestExpiredVerificationLink(t *testing.T) {
	e := newEnv(t, withSMTP)
	capture := &captureMailer{}
	e.setMailer(capture)
	e.user("alice@example.com", "alice-pass", false)
	c := e.browser()
	e.api(c, http.MethodPost, "/api/login", map[string]string{"email": "alice@example.com", "password": "alice-pass"})
	e.api(c, http.MethodPost, "/api/account/verify-email", nil)
	token := tokenFrom(capture.last(t).Text)
	e.st.SetClock(func() time.Time { return time.Now().Add(31 * time.Minute) })
	if r := e.api(c, http.MethodPost, "/api/verify", map[string]string{"token": token}); r.status != http.StatusGone {
		t.Fatalf("expired link: %d %s", r.status, r.body)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	e := newEnv(t, withSMTP)
	capture := &captureMailer{}
	e.setMailer(capture)
	e.user("alice@example.com", "old-password", false)
	other := e.browser()
	e.api(other, http.MethodPost, "/api/login", map[string]string{"email": "alice@example.com", "password": "old-password"})

	c := e.browser()
	if r := e.api(c, http.MethodPost, "/api/forgot-password", map[string]string{"email": "alice@example.com"}); r.status != http.StatusOK {
		t.Fatalf("forgot: %d %s", r.status, r.body)
	}
	token := tokenFrom(capture.last(t).Text)

	anon := e.browser()
	if r := e.api(anon, http.MethodPost, "/api/reset-password", map[string]any{"token": token, "password": "short"}); r.status != http.StatusBadRequest {
		t.Fatalf("short password: %d %s", r.status, r.body)
	}
	if r := e.api(anon, http.MethodPost, "/api/reset-password", map[string]any{"token": token, "password": "new-password"}); r.status != http.StatusOK {
		t.Fatalf("reset: %d %s", r.status, r.body)
	}
	if r := e.api(anon, http.MethodPost, "/api/reset-password", map[string]any{"token": token, "password": "another-pass"}); r.status != http.StatusGone {
		t.Fatalf("reused reset link: %d %s", r.status, r.body)
	}
	if r := e.api(other, http.MethodGet, "/api/sessions", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("old session survived reset: %d", r.status)
	}
	fresh := e.browser()
	if r := e.api(fresh, http.MethodPost, "/api/login", map[string]string{"email": "alice@example.com", "password": "new-password"}); r.status != http.StatusOK {
		t.Fatalf("login with new password: %d %s", r.status, r.body)
	}
}

func TestForgotPasswordDoesNotRevealAccounts(t *testing.T) {
	e := newEnv(t, withSMTP)
	capture := &captureMailer{}
	e.setMailer(capture)
	c := e.browser()
	known := e.api(c, http.MethodPost, "/api/forgot-password", map[string]string{"email": "nobody@example.com"})
	unknown := e.api(e.browser(), http.MethodPost, "/api/forgot-password", map[string]string{"email": "nobody@example.com"})
	if known.status != http.StatusOK || unknown.status != http.StatusOK {
		t.Fatalf("status: %d %d", known.status, unknown.status)
	}
	if len(capture.sent) != 0 {
		t.Fatal("mail sent for an unknown account")
	}
}

func TestForgotPasswordWithoutSMTP(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	r := e.api(c, http.MethodPost, "/api/forgot-password", map[string]string{"email": "a@example.com"})
	if r.status != http.StatusConflict || !strings.Contains(string(r.body), "mail_not_configured") {
		t.Fatalf("expected mail_not_configured, got %d %s", r.status, r.body)
	}
}

func tokenFrom(text string) string {
	const marker = "token="
	i := strings.Index(text, marker)
	if i < 0 {
		return ""
	}
	rest := text[i+len(marker):]
	if end := strings.IndexAny(rest, "\r\n "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}
