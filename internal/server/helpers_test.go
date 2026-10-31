package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"xauth/internal/config"
	"xauth/internal/mailer"
	"xauth/internal/oidcprovider"
	"xauth/internal/password"
	"xauth/internal/server"
	"xauth/internal/store"
)

const rpCallback = "http://rp.test/callback"

type env struct {
	t      *testing.T
	issuer string
	st     *store.Store
	srv    *server.Server
}

// newEnv starts XAuth on a random loopback port with a fresh database.
func newEnv(t *testing.T, mutate ...func(*config.Config)) *env {
	t.Helper()
	password.Cost = 4
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Issuer = "http://" + ln.Addr().String()
	cfg.Dev = true
	cfg.DBPath = filepath.Join(t.TempDir(), "xauth.db")
	for _, m := range mutate {
		m(&cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := oidcprovider.New(ctx, cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(cfg, st, p, nil, log)
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Listener.Close()
	ts.Listener = ln
	ts.Start()
	t.Cleanup(func() { ts.Close(); st.Close() })
	return &env{t: t, issuer: cfg.Issuer, st: st, srv: srv}
}

func (e *env) setMailer(m mailer.Sender) { e.srv.SetMailer(m) }

func (e *env) user(email, pw string, admin bool) *store.User {
	e.t.Helper()
	hash, err := password.Hash(pw)
	if err != nil {
		e.t.Fatal(err)
	}
	u := &store.User{Email: email, Name: "Test " + email, PasswordHash: hash, IsAdmin: admin}
	if err := e.st.CreateUser(context.Background(), u); err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *env) app(typ string, redirect string) (*store.Client, string) {
	e.t.Helper()
	c := &store.Client{Name: "Test " + typ, Type: typ, RedirectURIs: []string{redirect},
		PostLogoutRedirectURIs: []string{"http://rp.test/bye"}, AllowRefresh: true}
	secret, err := e.st.CreateClient(context.Background(), c)
	if err != nil {
		e.t.Fatal(err)
	}
	return c, secret
}

// browser is an HTTP client with cookies that does not follow redirects.
func (e *env) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

// location returns the Location header resolved against base.
func (r resp) location(t *testing.T) *url.URL {
	t.Helper()
	loc := r.header.Get("Location")
	if loc == "" {
		t.Fatalf("expected redirect, got %d: %s", r.status, r.body)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func do(t *testing.T, c *http.Client, method, u string, body io.Reader, hdr map[string]string) resp {
	t.Helper()
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, header: res.Header, body: b}
}

func (e *env) get(c *http.Client, path string) resp {
	return do(e.t, c, http.MethodGet, e.abs(path), nil, nil)
}

// api sends a JSON request the way the web UI does.
func (e *env) api(c *http.Client, method, path string, v any) resp {
	var body io.Reader
	if v != nil {
		b, _ := json.Marshal(v)
		body = bytes.NewReader(b)
	} else {
		body = strings.NewReader("{}")
	}
	return do(e.t, c, method, e.abs(path), body, map[string]string{
		"Content-Type": "application/json", "Sec-Fetch-Site": "same-origin",
	})
}

func (e *env) abs(p string) string {
	if strings.HasPrefix(p, "http") {
		return p
	}
	return e.issuer + p
}
