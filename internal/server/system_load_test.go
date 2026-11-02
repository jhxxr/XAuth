package server

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"xauth/internal/config"
	"xauth/internal/oidcprovider"
	"xauth/internal/store"
)

func TestStoredSMTPLoadsOnStart(t *testing.T) {
	ctx := context.Background()
	cfg := config.Defaults()
	cfg.Dev = true
	cfg.Issuer = "http://127.0.0.1:9"
	cfg.DBPath = filepath.Join(t.TempDir(), "xauth.db")
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	const raw = `{"host":"smtp.example","port":2525,"user":"u","pass":"pw","from":"a@b.c","tls":true}`
	if err := st.WriteSetting(ctx, store.SettingSMTP, raw); err != nil {
		t.Fatal(err)
	}
	p, err := oidcprovider.New(ctx, cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, st, p, nil, log)
	if !srv.mailEnabled() {
		t.Fatal("saved smtp was not loaded")
	}
	view := srv.smtpView()
	if view.Source != "database" || !view.PasswordSet || view.Host != "smtp.example" || view.Port != 2525 {
		t.Fatalf("loaded view: %+v", view)
	}
}

func TestStoredXMailLoadsOnStart(t *testing.T) {
	ctx := context.Background()
	cfg := config.Defaults()
	cfg.Dev = true
	cfg.Issuer = "http://127.0.0.1:9"
	cfg.DBPath = filepath.Join(t.TempDir(), "xauth.db")
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	const raw = `{"baseUrl":"https://mail.example","apiKey":"sk_live_saved"}`
	if err := st.WriteSetting(ctx, store.SettingXMail, raw); err != nil {
		t.Fatal(err)
	}
	p, err := oidcprovider.New(ctx, cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(cfg, st, p, nil, log)
	view := srv.xmailView()
	if !view.Enabled || !view.KeySet || view.Source != "database" || view.BaseURL != "https://mail.example" {
		t.Fatalf("loaded xmail: %+v", view)
	}
	if srv.xmailClient() == nil {
		t.Fatal("xmail client was not built from the saved settings")
	}
}
