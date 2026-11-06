package store

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"xauth/internal/outlook"
)

func TestOutlookStoreAndEncryption(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)

	u1 := &User{Email: "alice@example.com", Name: "Alice", PasswordHash: "hash", IsAdmin: true}
	u2 := &User{Email: "bob@example.com", Name: "Bob", PasswordHash: "hash"}
	for _, u := range []*User{u1, u2} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
	}

	// Default group exists
	groups, err := st.ListOutlookGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].ID != 1 {
		t.Fatalf("expected default group 1, got %+v (err=%v)", groups, err)
	}
	if err := st.DeleteOutlookGroup(ctx, 1); !errors.Is(err, ErrDefaultGroup) {
		t.Fatalf("expected ErrDefaultGroup when deleting group 1, got %v", err)
	}

	g2, err := st.CreateOutlookGroup(ctx, "研发组", "研发专用邮箱", "#4f46e5")
	if err != nil {
		t.Fatalf("CreateOutlookGroup: %v", err)
	}

	parsed, parseErrs := outlook.ParseAccountsText(`
box1@outlook.com----SecretPass123----cid-001----rt-secret-token-001
box2@hotmail.com--------cid-002----rt-secret-token-002
`)
	if len(parseErrs) != 0 || len(parsed) != 2 {
		t.Fatalf("unexpected parse result: %+v %+v", parsed, parseErrs)
	}

	added, updated, skipped, err := st.ImportOutlookAccounts(ctx, parsed, g2.ID, "首批导入", []string{u1.ID}, false, u1.ID)
	if err != nil || added != 2 || updated != 0 || skipped != 0 {
		t.Fatalf("ImportOutlookAccounts: added=%d updated=%d skipped=%d err=%v", added, updated, skipped, err)
	}

	// Verify ciphertext in SQLite does not equal plaintext
	var rawPwd, rawRT []byte
	if err := st.db.QueryRowContext(ctx, `SELECT password_encrypted, refresh_token_encrypted FROM outlook_accounts WHERE email = 'box1@outlook.com'`).Scan(&rawPwd, &rawRT); err != nil {
		t.Fatalf("query raw encrypted bytes: %v", err)
	}
	if bytes.Contains(rawPwd, []byte("SecretPass123")) || bytes.Contains(rawRT, []byte("rt-secret-token-001")) {
		t.Fatalf("plaintext leaked into encrypted columns")
	}

	// List accounts does not expose secrets
	list, err := st.ListOutlookAccounts(ctx, g2.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListOutlookAccounts: len=%d err=%v", len(list), err)
	}
	for _, a := range list {
		if a.Password != "" || a.RefreshToken != "" {
			t.Fatalf("ListOutlookAccounts leaked secret: %+v", a)
		}
	}

	// User 1 has access to both mailboxes; User 2 has access to none
	u1Boxes, err := st.UserOutlookMailboxes(ctx, u1.ID)
	if err != nil || len(u1Boxes) != 2 {
		t.Fatalf("UserOutlookMailboxes u1: %+v err=%v", u1Boxes, err)
	}
	u2Boxes, err := st.UserOutlookMailboxes(ctx, u2.ID)
	if err != nil || len(u2Boxes) != 0 {
		t.Fatalf("UserOutlookMailboxes u2: %+v err=%v", u2Boxes, err)
	}

	if _, err := st.UserGrantedOutlookAccount(ctx, u2.ID, "box1@outlook.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unassigned user u2, got %v", err)
	}
	grantedAcc, err := st.UserGrantedOutlookAccount(ctx, u1.ID, "BOX1@outlook.com")
	if err != nil || grantedAcc.RefreshToken != "rt-secret-token-001" || grantedAcc.Password != "SecretPass123" {
		t.Fatalf("UserGrantedOutlookAccount u1 failed: %+v err=%v", grantedAcc, err)
	}

	// Deleting custom group g2 moves accounts back to default group 1
	if err := st.DeleteOutlookGroup(ctx, g2.ID); err != nil {
		t.Fatalf("DeleteOutlookGroup: %v", err)
	}
	afterMove, err := st.OutlookAccountByEmail(ctx, "box1@outlook.com", false)
	if err != nil || afterMove.GroupID != 1 {
		t.Fatalf("expected account moved to group 1, got %+v err=%v", afterMove, err)
	}
}
