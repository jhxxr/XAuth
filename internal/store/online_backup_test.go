package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"xauth/internal/outlook"
)

func TestOnlineSnapshotRestoresEncryptedSecretsAndTheirKeys(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "active-source.db")
	st, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin := &User{Email: "backup@example.com", Name: "Backup", PasswordHash: "hash", IsAdmin: true}
	if err := st.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}

	entry := &TotpEntry{OwnerUserID: admin.ID, Name: "Restore drill", Digits: 6, Period: 30, Algorithm: "SHA1", CreatedBy: admin.ID}
	if err := st.CreateTotpEntry(ctx, entry, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	parsed, err := outlook.ParseAccountLine("backup-mail@example.com----mail-password----client-id----refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	if added, _, _, err := st.ImportOutlookAccounts(ctx, []outlook.ParsedAccount{parsed}, 1, "", []string{admin.ID}, false, admin.ID); err != nil || added != 1 {
		t.Fatalf("import Outlook test credential: added=%d err=%v", added, err)
	}
	if err := st.WriteSetting(ctx, SettingSMTP, `{"password":"independent application credential"}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateSession(ctx, admin.ID, time.Hour, SessionMeta{AMR: []string{"webauthn"}}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "snapshot.db")
	if err := BackupDatabase(ctx, databasePath, path); err != nil {
		t.Fatalf("make online SQLite snapshot: %v", err)
	}
	if err := ValidateBackupDatabase(ctx, path); err != nil {
		t.Fatalf("validate online SQLite snapshot: %v", err)
	}

	recovered, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open isolated restore: %v", err)
	}
	defer recovered.Close()
	account, err := recovered.OutlookAccountByEmail(ctx, parsed.Email, true)
	if err != nil || account.Password != parsed.Password || account.RefreshToken != parsed.RefreshToken {
		t.Fatalf("restored Outlook credential could not be decrypted: account=%+v err=%v", account, err)
	}
	codes, err := recovered.TotpCodes(ctx, admin.ID)
	if err != nil || len(codes) != 1 || codes[0].Code == "" {
		t.Fatalf("restored TOTP credential could not be decrypted: codes=%+v err=%v", codes, err)
	}
	var smtp string
	if err := recovered.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", SettingSMTP).Scan(&smtp); err != nil || smtp == "" {
		t.Fatalf("restored SMTP settings: value=%q err=%v", smtp, err)
	}
}

func TestPasswordResetLinkAndRevocationsRollBackTogether(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := &User{Email: "atomic-reset@example.com", PasswordHash: "old-hash"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateEmailToken(ctx, u.ID, EmailReset, EmailLinkTTL)
	if err != nil {
		t.Fatal(err)
	}
	cookie, _, err := st.CreateSession(ctx, u.ID, time.Hour, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Name: "Reset test", Type: ClientWeb, RedirectURIs: []string{"https://client.example/callback"}}
	if _, err := st.CreateClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	access := &AccessToken{FamilyID: "reset-family", ClientID: client.ID, UserID: u.ID}
	refresh := &RefreshToken{FamilyID: "reset-family", ClientID: client.ID, UserID: u.ID}
	rawRefresh, err := st.IssueTokens(ctx, access, time.Hour, refresh, 24*time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `CREATE TRIGGER reject_access_revocation BEFORE UPDATE OF revoked_at ON access_tokens BEGIN SELECT RAISE(ABORT, 'forced test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetPasswordWithEmailToken(ctx, token, "new-hash"); err == nil {
		t.Fatal("reset succeeded despite the deliberately rejected password update")
	}
	var passwordHash string
	var used sql.NullInt64
	if err := st.db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id = ?", u.ID).Scan(&passwordHash); err != nil || passwordHash != "old-hash" {
		t.Fatalf("failed reset changed the password: hash=%q err=%v", passwordHash, err)
	}
	if err := st.db.QueryRowContext(ctx, "SELECT used_at FROM email_tokens WHERE token_hash = ?", HashToken(token)).Scan(&used); err != nil || used.Valid {
		t.Fatalf("failed reset consumed its one-time link: used=%v err=%v", used, err)
	}
	if _, err := st.SessionByToken(ctx, cookie); err != nil {
		t.Fatalf("failed reset revoked the existing session: %v", err)
	}
	if current, err := st.AccessTokenByID(ctx, access.ID); err != nil || !current.Active(st.Now()) {
		t.Fatalf("failed reset revoked its access token: token=%+v err=%v", current, err)
	}
	if current, err := st.RefreshTokenByRaw(ctx, rawRefresh); err != nil || !current.Active(st.Now()) {
		t.Fatalf("failed reset revoked its refresh token: token=%+v err=%v", current, err)
	}
	if _, err := st.db.ExecContext(ctx, "DROP TRIGGER reject_access_revocation"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetPasswordWithEmailToken(ctx, token, "new-hash"); err != nil {
		t.Fatalf("successful reset: %v", err)
	}
	if _, err := st.SessionByToken(ctx, cookie); !errors.Is(err, ErrNotFound) {
		t.Fatalf("successful reset retained the old session: %v", err)
	}
	if current, err := st.AccessTokenByID(ctx, access.ID); err != nil || current.Active(st.Now()) {
		t.Fatalf("successful reset kept its access token active: token=%+v err=%v", current, err)
	}
	if current, err := st.RefreshTokenByRaw(ctx, rawRefresh); err != nil || current.Active(st.Now()) {
		t.Fatalf("successful reset kept its refresh token active: token=%+v err=%v", current, err)
	}
	if err := st.db.QueryRowContext(ctx, "SELECT used_at FROM email_tokens WHERE token_hash = ?", HashToken(token)).Scan(&used); err != nil || !used.Valid {
		t.Fatalf("successful reset did not consume its link: used=%v err=%v", used, err)
	}
}

func TestSessionAuthenticationMethodRoundTrips(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := &User{Email: "amr@example.com", PasswordHash: "hash"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	raw, session, err := st.CreateSession(ctx, u.ID, time.Hour, SessionMeta{AMR: []string{"webauthn"}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := st.SessionByToken(ctx, raw)
	if err != nil || len(loaded.AMR) != 1 || loaded.AMR[0] != "webauthn" || loaded.ID != session.ID {
		t.Fatalf("session lost its authentication method: session=%+v err=%v", loaded, err)
	}
	if _, _, err := st.CreateSession(ctx, u.ID, time.Hour, SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	sessions, err := st.ListSessions(ctx, u.ID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("list old/new sessions: count=%d err=%v", len(sessions), err)
	}
	for _, candidate := range sessions {
		if candidate.ID != session.ID && len(candidate.AMR) != 0 {
			t.Fatalf("an unknown authentication method was invented: %v", candidate.AMR)
		}
	}
}

func TestUserPolicyAndCredentialRevocationRollBackTogether(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := &User{Email: "atomic-user-policy@example.com", PasswordHash: "old-hash", IsAdmin: true}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(ctx, &User{Email: "another-admin@example.com", PasswordHash: "hash", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	client := &Client{Name: "Admin-only test", Type: ClientWeb, Access: AccessAdmins, RedirectURIs: []string{"https://client.example/callback"}}
	if _, err := st.CreateClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	access := &AccessToken{FamilyID: "admin-revoke-family", ClientID: client.ID, UserID: u.ID}
	refresh := &RefreshToken{FamilyID: access.FamilyID, ClientID: client.ID, UserID: u.ID}
	rawRefresh, err := st.IssueTokens(ctx, access, time.Hour, refresh, time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	rawSession, _, err := st.CreateSession(ctx, u.ID, time.Hour, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `CREATE TRIGGER reject_user_token_revocation BEFORE UPDATE OF revoked_at ON access_tokens BEGIN SELECT RAISE(ABORT, 'forced test failure'); END`); err != nil {
		t.Fatal(err)
	}
	u.Disabled, u.IsAdmin = true, false
	if err := st.UpdateUserWithAccessRevocation(ctx, u, "", true, true); err == nil {
		t.Fatal("user update succeeded despite the deliberately rejected revocation")
	}
	current, err := st.UserByID(ctx, u.ID)
	if err != nil || current.Disabled || !current.IsAdmin || current.PasswordHash != "old-hash" {
		t.Fatalf("failed policy update partially changed the account: user=%+v err=%v", current, err)
	}
	if _, err := st.SessionByToken(ctx, rawSession); err != nil {
		t.Fatalf("failed policy update removed the existing session: %v", err)
	}
	if current, err := st.AccessTokenByID(ctx, access.ID); err != nil || !current.Active(st.Now()) {
		t.Fatalf("failed policy update revoked the access token: token=%+v err=%v", current, err)
	}
	if current, err := st.RefreshTokenByRaw(ctx, rawRefresh); err != nil || !current.Active(st.Now()) {
		t.Fatalf("failed policy update revoked the refresh token: token=%+v err=%v", current, err)
	}
}
