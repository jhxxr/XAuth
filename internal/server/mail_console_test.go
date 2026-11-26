package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"xauth/internal/config"
	"xauth/internal/store"
)

// Exercise the exact proxy operations used by the registration console through
// a real XAuth session, including credentials, sandboxed HTML and audit events.
func TestAdminMailConsoleWorkflow(t *testing.T) {
	const key = "console-upstream-key"
	const address = "registration@ops.example.com"
	const nextAddress = "next@ops.example.com"
	created := 0
	deleted := map[string]bool{}
	xm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("xmail request did not carry the server-side API key")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/v1")
		switch {
		case r.Method == http.MethodGet && path == "/system/domains":
			_, _ = w.Write([]byte(`{"success":true,"data":{"domains":["example.com","ops.example.com"],"defaultDomain":"ops.example.com"}}`))
		case r.Method == http.MethodPost && path == "/mailboxes/batch":
			var body struct {
				Count  int    `json:"count"`
				Domain string `json:"domain"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Count != 1 || body.Domain != "ops.example.com" {
				t.Errorf("unexpected batch create body: %+v, error: %v", body, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			created++
			current := address
			if created > 1 {
				current = nextAddress
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"success":true,"data":{"count":1,"items":[{"address":"` + current + `","password":"generated-mailbox-password"}]}}`))
		case r.Method == http.MethodGet && path == "/mailboxes/"+address:
			if deleted[address] {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"success":false,"error":"mailbox deleted"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"mailbox":{"address":"` + address + `","isActive":true},"stats":{"total":1,"unread":1},"services":[]}}`))
		case r.Method == http.MethodGet && path == "/mailboxes/"+nextAddress+"/password":
			_, _ = w.Write([]byte(`{"success":true,"data":{"address":"` + nextAddress + `","password":"generated-mailbox-password","hasPassword":true}}`))
		case r.Method == http.MethodGet && path == "/mailboxes/"+address+"/emails":
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"id":"console-message","mailbox":"` + address + `","subject":"Registration code","verificationCode":"123456"}]}}`))
		case r.Method == http.MethodGet && path == "/mailboxes/"+address+"/verification-code":
			if r.URL.Query().Get("seconds") != "600" {
				t.Error("verification code freshness window was lost")
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"code":"123456","emailId":"console-message"}}`))
		case r.Method == http.MethodGet && path == "/emails/console-message":
			_, _ = w.Write([]byte(`{"success":true,"data":{"email":{"id":"console-message","mailbox":"` + address + `","subject":"Registration code","html":"<p>Your code is <b>123456</b></p>","text":"Your code is 123456"}}}`))
		case r.Method == http.MethodPost && path == "/emails/console-message/read":
			_, _ = w.Write([]byte(`{"success":true,"data":{"email":{"id":"console-message","isRead":true}}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/mailboxes/"):
			deleted[strings.TrimPrefix(path, "/mailboxes/")] = true
			_, _ = w.Write([]byte(`{"success":true,"data":{"deleted":true}}`))
		default:
			t.Errorf("unexpected console upstream operation: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer xm.Close()
	e := newEnv(t, func(cfg *config.Config) {
		cfg.XMailBaseURL = xm.URL
		cfg.XMailAPIKey = config.Secret(key)
	})
	admin := e.user("console-admin@example.com", "correct horse", true)
	browser := e.browser()
	login(e, browser, admin.Email, "correct horse")

	steps := []struct {
		method string
		path   string
		body   any
		status int
	}{
		{http.MethodGet, "/api/admin/mail/system/domains", nil, http.StatusOK},
		{http.MethodPost, "/api/admin/mail/mailboxes/batch", map[string]any{"count": 1, "domain": "ops.example.com"}, http.StatusCreated},
		{http.MethodGet, "/api/admin/mail/mailboxes/" + address, nil, http.StatusOK},
		{http.MethodGet, "/api/admin/mail/mailboxes/" + address + "/emails?limit=50", nil, http.StatusOK},
		{http.MethodGet, "/api/admin/mail/mailboxes/" + address + "/verification-code?seconds=600", nil, http.StatusOK},
		{http.MethodGet, "/api/admin/mail/emails/console-message", nil, http.StatusOK},
		{http.MethodPost, "/api/admin/mail/emails/console-message/read", nil, http.StatusOK},
		// Creating the next mailbox must preserve the first one.
		{http.MethodPost, "/api/admin/mail/mailboxes/batch", map[string]any{"count": 1, "domain": "ops.example.com"}, http.StatusCreated},
		{http.MethodGet, "/api/admin/mail/mailboxes/" + address, nil, http.StatusOK},
		{http.MethodGet, "/api/admin/mail/mailboxes/" + nextAddress + "/password", nil, http.StatusOK},
		{http.MethodDelete, "/api/admin/mail/mailboxes/" + nextAddress, nil, http.StatusOK},
		{http.MethodGet, "/api/admin/mail/mailboxes/" + address, nil, http.StatusOK},
	}
	for _, step := range steps {
		r := e.api(browser, step.method, step.path, step.body)
		if r.status != step.status {
			t.Fatalf("%s %s: %d %s", step.method, step.path, r.status, r.body)
		}
		if strings.Contains(string(r.body), key) {
			t.Fatal("upstream API key leaked to the console")
		}
		if step.method == http.MethodPost && strings.HasSuffix(step.path, "/batch") {
			var result struct {
				Items []struct {
					Address  string `json:"address"`
					Password string `json:"password"`
				} `json:"items"`
			}
			r.json(t, &result)
			expected := address
			if created > 1 {
				expected = nextAddress
			}
			if len(result.Items) != 1 || result.Items[0].Address != expected || result.Items[0].Password == "" {
				t.Fatalf("generated credentials were not preserved: %s", r.body)
			}
		}
	}
	if created != 2 || !deleted[nextAddress] || deleted[address] {
		t.Fatalf("new creation must preserve the previous mailbox, and deletion must only affect the current one: created=%d, deleted=%v", created, deleted)
	}

	html := e.get(browser, "/api/admin/mail-html/console-message")
	if html.status != http.StatusOK || !strings.Contains(string(html.body), "<b>123456</b>") || !strings.Contains(html.header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("console HTML must use the sandboxed reader: %d %s", html.status, html.body)
	}
	events, err := e.st.ListEvents(t.Context(), store.EventFilter{Types: []string{store.EvMailAdminView}})
	if err != nil {
		t.Fatal(err)
	}
	var mailboxView, messageView bool
	for _, event := range events {
		if event.ActorID == admin.ID && event.SubjectID == address {
			if event.Detail["emailId"] == "console-message" {
				messageView = true
			} else {
				mailboxView = true
			}
		}
	}
	// Mailbox/code and detail/HTML views intentionally share deduplicated events.
	if !mailboxView || !messageView {
		t.Fatalf("missing console read audit events: mailbox=%v, message=%v", mailboxView, messageView)
	}
}

func TestAdminMailConsoleRejectsNonAdmins(t *testing.T) {
	var calls atomic.Int32
	xm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer xm.Close()
	e := newEnv(t, func(cfg *config.Config) {
		cfg.XMailBaseURL = xm.URL
		cfg.XMailAPIKey = "console-upstream-key"
	})
	user := e.user("console-user@example.com", "correct horse", false)
	normal := e.browser()
	login(e, normal, user.Email, "correct horse")
	for _, browser := range []struct {
		client *http.Client
		status int
	}{
		{e.browser(), http.StatusUnauthorized},
		{normal, http.StatusForbidden},
	} {
		for _, step := range []struct{ method, path string }{
			{http.MethodGet, "/api/admin/mail/system/domains"},
			{http.MethodGet, "/api/admin/mail/mailboxes"},
			{http.MethodPost, "/api/admin/mail/mailboxes/batch"},
			{http.MethodGet, "/api/admin/mail/mailboxes/registration@example.com/emails"},
			{http.MethodGet, "/api/admin/mail/mailboxes/registration@example.com/verification-code"},
			{http.MethodGet, "/api/admin/mail/mailboxes/registration@example.com/password"},
			{http.MethodGet, "/api/admin/mail/emails/console-message"},
			{http.MethodGet, "/api/admin/mail-html/console-message"},
			{http.MethodPatch, "/api/admin/mail/mailboxes/registration@example.com"},
			{http.MethodDelete, "/api/admin/mail/mailboxes/registration@example.com"},
		} {
			r := e.api(browser.client, step.method, step.path, nil)
			if r.status != browser.status {
				t.Errorf("%s %s: expected %d, got %d", step.method, step.path, browser.status, r.status)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unauthorized console requests reached xmail: %d", calls.Load())
	}
}
