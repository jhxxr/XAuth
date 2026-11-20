package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"xauth/internal/config"
	"xauth/internal/oidcprovider"
	"xauth/internal/outlook"
	"xauth/internal/server"
	"xauth/internal/store"
)

func TestEncryptedBackupRestoresIntoIsolatedSQLiteInstance(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "production.db")
	backupPath := filepath.Join(root, "encrypted.db.age")
	isolated := t.TempDir()
	restoredPath := filepath.Join(isolated, "restore.db")
	t.Setenv("XAUTH_DB", database)
	identity, identityPath := makeBackupIdentity(t, isolated)
	t.Setenv("XAUTH_BACKUP_RECIPIENT", identity.Recipient().String())
	backupArgs := []string{"-output", backupPath}
	restoreArgs := func(input, key, target string) []string {
		return []string{"-input", input, "-identity", key, "-db", target}
	}
	ctx := context.Background()

	st, err := store.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	admin := &store.User{Email: "backup-drill@example.com", Name: "Backup drill", PasswordHash: "stored-hash", IsAdmin: true}
	if err := st.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateTotpEntry(ctx, &store.TotpEntry{
		OwnerUserID: admin.ID, Name: "Restore verifier", Digits: 6, Period: 30, Algorithm: "SHA1", CreatedBy: admin.ID,
	}, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	account, err := outlook.ParseAccountLine("restore-mail@example.com----outlook-password-drill----client-id-drill----refresh-token-drill")
	if err != nil {
		t.Fatal(err)
	}
	if added, _, _, err := st.ImportOutlookAccounts(ctx, []outlook.ParsedAccount{account}, 1, "", []string{admin.ID}, false, admin.ID); err != nil || added != 1 {
		t.Fatalf("import backup test mailbox: added=%d err=%v", added, err)
	}
	defer st.Close()
	cfg := config.Defaults()
	cfg.Dev = true
	if _, err := oidcprovider.New(ctx, cfg, st); err != nil {
		t.Fatal(err)
	}
	keys, err := st.AllSigningKeys(ctx)
	if err != nil || len(keys) != 1 {
		t.Fatalf("create source signing key: %v", err)
	}
	if err := st.WriteSetting(ctx, store.SettingSMTP, `{"password":"stored SMTP password"}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateSession(ctx, admin.ID, time.Hour, store.SessionMeta{AMR: []string{"webauthn"}}); err != nil {
		t.Fatal(err)
	}
	// Keep the source open in WAL mode during backup.
	if err := runBackup(backupArgs); err != nil {
		t.Fatalf("create encrypted SQLite snapshot: %v", err)
	}
	if err := runRestore(restoreArgs(backupPath, identityPath, restoredPath)); err != nil {
		t.Fatalf("decrypt and verify into isolated instance: %v", err)
	}
	if _, err := os.Stat(restoredPath); err != nil {
		t.Fatalf("restored SQLite file was not published: %v", err)
	}
	if err := store.ValidateBackupDatabase(ctx, restoredPath); err != nil {
		t.Fatalf("isolated restore did not pass database integrity and schema checks: %v", err)
	}

	recovered, err := store.Open(ctx, restoredPath)
	if err != nil {
		t.Fatalf("open isolated XAuth instance: %v", err)
	}
	defer recovered.Close()
	restoredAdmin, err := recovered.UserByEmail(ctx, admin.Email)
	if err != nil || restoredAdmin.PasswordHash != admin.PasswordHash {
		t.Fatalf("restore omitted user credentials: user=%+v err=%v", restoredAdmin, err)
	}
	restoredMail, err := recovered.OutlookAccountByEmail(ctx, account.Email, true)
	if err != nil || restoredMail.Password != account.Password || restoredMail.RefreshToken != account.RefreshToken {
		t.Fatalf("restore omitted the Outlook encryption key: mailbox=%+v err=%v", restoredMail, err)
	}
	if codes, err := recovered.TotpCodes(ctx, admin.ID); err != nil || len(codes) != 1 || codes[0].Code == "" {
		t.Fatalf("restore omitted the TOTP encryption key: codes=%+v err=%v", codes, err)
	}
	if _, ok, err := recovered.ReadSetting(ctx, store.SettingSMTP); err != nil || !ok {
		t.Fatalf("restored database could not read SMTP credentials: %v", err)
	}
	provider, err := oidcprovider.New(ctx, cfg, recovered)
	if err != nil {
		t.Fatalf("start recovered OIDC provider: %v", err)
	}
	handler := server.New(cfg, recovered, provider, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, cfg.Issuer+"/keys", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), keys[0].ID) {
		t.Fatalf("recovered provider did not publish the original signing key: status=%d", w.Code)
	}
	if err := runBackup(backupArgs); err == nil {
		t.Fatal("backup command overwrote an existing encrypted backup")
	}
	if err := runRestore(restoreArgs(backupPath, identityPath, restoredPath)); err == nil {
		t.Fatal("restore overwrote the existing isolated database")
	}

	_, wrongIdentity := makeBackupIdentity(t, t.TempDir())
	failedPath := filepath.Join(isolated, "must-not-be-installed.db")
	if err := runRestore(restoreArgs(backupPath, wrongIdentity, failedPath)); err == nil {
		t.Fatal("restore accepted an incorrect recovery identity")
	}
	if _, err := os.Stat(failedPath); !os.IsNotExist(err) {
		t.Fatalf("failed restore left a published database: err=%v", err)
	}
	backupBytes, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	// The file is decryptable with the standard age API, not an XAuth-only format.
	decoded, err := age.Decrypt(bytes.NewReader(backupBytes), identity)
	if err != nil {
		t.Fatal(err)
	}
	var header [16]byte
	if _, err := io.ReadFull(decoded, header[:]); err != nil || string(header[:]) != "SQLite format 3\x00" {
		t.Fatalf("decrypt SQLite file using age: %v", err)
	}
	truncated := filepath.Join(root, "truncated.db.age")
	if err := os.WriteFile(truncated, backupBytes[:len(backupBytes)-1], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runRestore(restoreArgs(truncated, identityPath, failedPath)); err == nil {
		t.Fatal("restore accepted a truncated backup")
	}
	if _, err := os.Stat(failedPath); !os.IsNotExist(err) {
		t.Fatalf("truncated restore left a published database: err=%v", err)
	}
}

func makeBackupIdentity(t *testing.T, dir string) (*age.X25519Identity, string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "recovery.key")
	if err := os.WriteFile(path, []byte("# XAuth restore test identity\n"+id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return id, path
}
