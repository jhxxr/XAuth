package xmail

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClientURLAndRoutes(t *testing.T) {
	c, err := New("https://mail.example.com", "sk_live_test.123")
	if err != nil {
		t.Fatal(err)
	}

	u := c.URL([]string{"mailboxes", "test demo@example.com", "emails"}, url.Values{"limit": {"10"}})
	expected := "https://mail.example.com/api/v1/mailboxes/test%20demo@example.com/emails?limit=10"
	if u != expected {
		t.Fatalf("expected %q, got %q", expected, u)
	}

	// Route allowance checks
	if !AdminAllowed("GET", []string{"mailboxes"}) {
		t.Fatal("GET mailboxes should be allowed")
	}
	if !AdminAllowed("GET", []string{"mailboxes", "demo@example.com"}) {
		t.Fatal("GET mailboxes/:addr should be allowed")
	}
	if !AdminAllowed("POST", []string{"mailboxes", "demo@example.com", "restore"}) {
		t.Fatal("POST mailboxes/:addr/restore should be allowed")
	}
	if !AdminAllowed("GET", []string{"mailboxes", "demo@example.com", "verification-code"}) {
		t.Fatal("GET verification-code should be allowed")
	}
	if !AdminAllowed("POST", []string{"emails", "msg123", "star"}) {
		t.Fatal("POST emails/:id/star should be allowed")
	}
	if !AdminAllowed("GET", []string{"system", "settings"}) {
		t.Fatal("GET system/settings should be allowed")
	}

	// Disallowed routes
	if AdminAllowed("GET", []string{"evil"}) {
		t.Fatal("GET evil should not be allowed")
	}
	if AdminAllowed("DELETE", []string{"system", "settings"}) {
		t.Fatal("DELETE system/settings should not be allowed")
	}
	if AdminAllowed("GET", []string{".."}) {
		t.Fatal(".. should not be allowed")
	}
	if AdminAllowed("GET", []string{"mailboxes", "..", "emails"}) {
		t.Fatal(".. segment should not be allowed")
	}
}

func TestClientDo(t *testing.T) {
	var receivedAuth string
	var receivedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/mailboxes":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"address":"demo@example.com"}]}}`))
		case "/api/v1/fail":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"success":false,"error":"bad parameter"}`))
		case "/api/v1/invalid-json":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
		case "/api/v1/too-large":
			w.WriteHeader(http.StatusOK)
			// write more than MaxResponse (4MB)
			w.Write([]byte(`{"success":true,"data":"`))
			w.Write([]byte(strings.Repeat("a", MaxResponse+10)))
			w.Write([]byte(`"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c, err := New(ts.URL, "sk_live_dummy.key")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// 1. Success case
	res, err := c.Get(ctx, []string{"mailboxes"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() || res.Status != 200 {
		t.Fatalf("expected OK, got: status=%d success=%v", res.Status, res.Success)
	}
	if receivedAuth != "Bearer sk_live_dummy.key" {
		t.Fatalf("unexpected Authorization header: %q", receivedAuth)
	}
	if receivedPath != "/api/v1/mailboxes" {
		t.Fatalf("unexpected path: %q", receivedPath)
	}

	var data struct {
		Items []struct {
			Address string `json:"address"`
		} `json:"items"`
	}
	if err := res.Decode(&data); err != nil || len(data.Items) != 1 || data.Items[0].Address != "demo@example.com" {
		t.Fatalf("decode failed or unexpected items: %+v, err=%v", data, err)
	}

	// 2. Failure from xmail
	res, err = c.Get(ctx, []string{"fail"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() || res.Status != 400 || res.Error != "bad parameter" {
		t.Fatalf("expected error result: %+v", res)
	}

	// 3. Invalid JSON
	_, err = c.Get(ctx, []string{"invalid-json"}, nil)
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("expected ErrBadResponse, got: %v", err)
	}

	// 4. Too large
	_, err = c.Get(ctx, []string{"too-large"}, nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got: %v", err)
	}

	// 5. Invalid segment with .. or empty
	_, err = c.Get(ctx, []string{".."}, nil)
	if err == nil {
		t.Fatal("expected error for segment '..'")
	}
}
