package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xauth/internal/config"
	"xauth/internal/store"
)

func TestMailUnconfigured(t *testing.T) {
	e := newEnv(t)
	u := e.user("user@example.com", "correct horse", false)
	c := e.browser()
	login(e, c, u.Email, "correct horse")

	var status struct {
		Configured bool `json:"configured"`
	}
	r := e.api(c, http.MethodGet, "/api/mail/status", nil)
	if r.status != http.StatusOK {
		t.Fatalf("mail status: %d %s", r.status, r.body)
	}
	r.json(t, &status)
	if status.Configured {
		t.Fatal("expected mail to be unconfigured")
	}

	r = e.api(c, http.MethodGet, "/api/mail/mailboxes", nil)
	if r.status != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for unconfigured mail, got: %d %s", r.status, r.body)
	}
}

func fakeXMailServer(t *testing.T, apiKey string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"error":"Unauthorized"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")

		switch {
		case p == "/mailboxes" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"address":"alice@example.com","isActive":true,"createdAt":1700000000}]}}`))
		case p == "/mailboxes/alice@example.com" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"mailbox":{"address":"alice@example.com","isActive":true,"createdAt":1700000000},"stats":{"total":1,"unread":1},"services":[{"id":"s1","name":"GitHub","loginUrl":"https://github.com/login"}]}}`))
		case p == "/mailboxes/bob@example.com" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"mailbox":{"address":"bob@example.com","isActive":true,"createdAt":1700000000},"stats":{"total":0,"unread":0},"services":[]}}`))
		case p == "/mailboxes/alice@example.com/emails" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"id":"msg-alice-1","mailbox":"alice@example.com","from":"noreply@service.com","subject":"Welcome","isRead":false,"isStarred":false,"createdAt":1700000010,"preview":"Hello Alice","verificationCode":"123456"}]}}`))
		case p == "/mailboxes/bob@example.com/emails" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"id":"msg-bob-1","mailbox":"bob@example.com","from":"noreply@service.com","subject":"Bob Note","isRead":true,"isStarred":false,"createdAt":1700000020,"preview":"Hello Bob","verificationCode":null}]}}`))
		case p == "/mailboxes/alice@example.com/verification-code" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"code":"123456","subject":"Welcome","from":"noreply@service.com","receivedAt":1700000010,"emailId":"msg-alice-1"}}`))
		case p == "/emails/msg-alice-1" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"email":{"id":"msg-alice-1","mailbox":"alice@example.com","from":"noreply@service.com","subject":"Welcome","text":"Hello Alice, code is 123456","html":"<p>Hello Alice, code is <strong>123456</strong></p>","isRead":false,"isStarred":false,"createdAt":1700000010}}}`))
		case p == "/emails/msg-bob-1" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"email":{"id":"msg-bob-1","mailbox":"bob@example.com","from":"noreply@service.com","subject":"Bob Note","text":"Hello Bob","html":null,"isRead":true,"isStarred":false,"createdAt":1700000020}}}`))
		case p == "/emails/msg-alice-1/read" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"email":{"id":"msg-alice-1","mailbox":"alice@example.com","isRead":true}}}`))
		case p == "/emails/msg-alice-1/star" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"email":{"id":"msg-alice-1","mailbox":"alice@example.com","isStarred":true}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"Not Found"}`))
		}
	}))
}

func TestMailIntegration(t *testing.T) {
	const secretKey = "sk_live_super_secret_key_12345"
	fakeXM := fakeXMailServer(t, secretKey)
	defer fakeXM.Close()

	e := newEnv(t, func(cfg *config.Config) {
		cfg.XMailBaseURL = fakeXM.URL
		cfg.XMailAPIKey = config.Secret(secretKey)
	})

	admin := e.user("admin@example.com", "correct horse", true)
	userA := e.user("alice_user@example.com", "correct horse", false)
	userB := e.user("bob_user@example.com", "correct horse", false)

	bAdmin := e.browser()
	login(e, bAdmin, admin.Email, "correct horse")

	bUserA := e.browser()
	login(e, bUserA, userA.Email, "correct horse")

	bUserB := e.browser()
	login(e, bUserB, userB.Email, "correct horse")

	// 1. Initial state: User A has no mailboxes
	var mbList struct {
		Mailboxes []struct {
			Address string `json:"address"`
		} `json:"mailboxes"`
	}
	r := e.api(bUserA, http.MethodGet, "/api/mail/mailboxes", nil)
	if r.status != http.StatusOK {
		t.Fatalf("list my mailboxes: %d %s", r.status, r.body)
	}
	r.json(t, &mbList)
	if len(mbList.Mailboxes) != 0 {
		t.Fatalf("expected 0 mailboxes, got: %d", len(mbList.Mailboxes))
	}

	// 2. Admin grants alice@example.com to userA
	r = e.api(bAdmin, http.MethodPut, "/api/admin/mail-grants/alice@example.com", map[string]any{
		"userIds": []string{userA.ID},
	})
	if r.status != http.StatusOK {
		t.Fatalf("admin grant: %d %s", r.status, r.body)
	}

	// User A now sees alice@example.com
	r = e.api(bUserA, http.MethodGet, "/api/mail/mailboxes", nil)
	r.json(t, &mbList)
	if len(mbList.Mailboxes) != 1 || mbList.Mailboxes[0].Address != "alice@example.com" {
		t.Fatalf("expected alice@example.com, got: %+v", mbList.Mailboxes)
	}

	// 3. User A accesses alice@example.com details and emails
	r = e.api(bUserA, http.MethodGet, "/api/mail/mailboxes/alice@example.com", nil)
	if r.status != http.StatusOK {
		t.Fatalf("get alice mailbox: %d %s", r.status, r.body)
	}

	r = e.api(bUserA, http.MethodGet, "/api/mail/mailboxes/alice@example.com/emails", nil)
	if r.status != http.StatusOK {
		t.Fatalf("get alice emails: %d %s", r.status, r.body)
	}
	var emails struct {
		Items []struct {
			ID               string  `json:"id"`
			VerificationCode *string `json:"verificationCode"`
		} `json:"items"`
	}
	r.json(t, &emails)
	if len(emails.Items) != 1 || emails.Items[0].ID != "msg-alice-1" || *emails.Items[0].VerificationCode != "123456" {
		t.Fatalf("unexpected emails: %+v", emails.Items)
	}

	// Verification code endpoint
	var codeResp struct {
		Code *string `json:"code"`
	}
	r = e.api(bUserA, http.MethodGet, "/api/mail/mailboxes/alice@example.com/verification-code", nil)
	if r.status != http.StatusOK {
		t.Fatalf("get verification code: %d %s", r.status, r.body)
	}
	r.json(t, &codeResp)
	if codeResp.Code == nil || *codeResp.Code != "123456" {
		t.Fatalf("expected code 123456, got: %+v", codeResp)
	}

	// Sandboxed HTML body
	rHTML := e.get(bUserA, "/api/mail/emails/msg-alice-1/html")
	if rHTML.status != http.StatusOK {
		t.Fatalf("get html: %d", rHTML.status)
	}
	csp := rHTML.header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("expected sandboxed CSP, got: %q", csp)
	}
	if !strings.Contains(string(rHTML.body), "<strong>123456</strong>") {
		t.Fatalf("expected html body to contain markup, got: %s", rHTML.body)
	}

	// Mark read and star
	r = e.api(bUserA, http.MethodPost, "/api/mail/emails/msg-alice-1/read", nil)
	if r.status != http.StatusOK {
		t.Fatalf("mark read: %d %s", r.status, r.body)
	}
	r = e.api(bUserA, http.MethodPost, "/api/mail/emails/msg-alice-1/star", map[string]bool{"isStarred": true})
	if r.status != http.StatusOK {
		t.Fatalf("star: %d %s", r.status, r.body)
	}

	// 4. Isolation: User A tries to guess Bob's mailbox or Bob's email -> 404
	r = e.api(bUserA, http.MethodGet, "/api/mail/mailboxes/bob@example.com", nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("expected 404 for ungranted mailbox, got: %d", r.status)
	}
	r = e.api(bUserA, http.MethodGet, "/api/mail/mailboxes/bob@example.com/emails", nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("expected 404 for ungranted mailbox emails, got: %d", r.status)
	}
	r = e.api(bUserA, http.MethodGet, "/api/mail/emails/msg-bob-1", nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("expected 404 for ungranted email, got: %d", r.status)
	}

	// 5. Admin Proxy:
	// Non-admin trying admin proxy gets 403
	r = e.api(bUserA, http.MethodGet, "/api/admin/mail/mailboxes", nil)
	if r.status != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin on proxy, got: %d", r.status)
	}

	// Admin proxy works
	r = e.api(bAdmin, http.MethodGet, "/api/admin/mail/mailboxes", nil)
	if r.status != http.StatusOK {
		t.Fatalf("admin proxy get mailboxes: %d %s", r.status, r.body)
	}

	// Proxy security: traversal & unwhitelisted routes rejected
	r = e.api(bAdmin, http.MethodGet, "/api/admin/mail/../other", nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("expected 404 for traversal in proxy, got: %d", r.status)
	}
	r = e.api(bAdmin, http.MethodGet, "/api/admin/mail/not-whitelisted-endpoint", nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("expected 404 for unwhitelisted proxy endpoint, got: %d", r.status)
	}

	// 6. Security check: Secret API key MUST NOT be anywhere in events or response bodies
	r = e.api(bAdmin, http.MethodGet, "/api/admin/events?group=mail", nil)
	if r.status != http.StatusOK {
		t.Fatalf("list mail events: %d %s", r.status, r.body)
	}
	if strings.Contains(string(r.body), secretKey) {
		t.Fatalf("SECRET KEY LEAKED IN AUDIT LOG: %s", r.body)
	}

	events, err := e.st.ListEvents(t.Context(), store.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		detailBytes, _ := json.Marshal(ev.Detail)
		if strings.Contains(string(detailBytes), secretKey) {
			t.Fatalf("SECRET KEY FOUND IN EVENT %d DETAIL", ev.ID)
		}
	}
}
