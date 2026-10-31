package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"xauth/internal/totp"
)

func TestTotpVisibilityAndGrants(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	admin := &User{Email: "admin@example.com", PasswordHash: "x", IsAdmin: true}
	alice := &User{Email: "alice@example.com", PasswordHash: "x"}
	bob := &User{Email: "bob@example.com", PasswordHash: "x"}
	for _, u := range []*User{admin, alice, bob} {
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	shared := &TotpEntry{Name: "GitHub", Issuer: "GitHub", AccountName: "ops", Digits: 6, Period: 30, Algorithm: totp.SHA1, CreatedBy: admin.ID}
	if err := s.CreateTotpEntry(ctx, shared, "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"); err != nil {
		t.Fatal(err)
	}
	personal := &TotpEntry{OwnerUserID: alice.ID, Name: "私人", Digits: 6, Period: 30, Algorithm: totp.SHA1, CreatedBy: alice.ID}
	if err := s.CreateTotpEntry(ctx, personal, "GEZDGNBVGY3TQOJQ"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.TotpEntryByID(ctx, shared.ID, bob.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ungranted shared entry visible: %v", err)
	}
	if _, err := s.TotpEntryByID(ctx, personal.ID, bob.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("admin could read a personal entry: %v", err)
	}
	if err := s.SetTotpGrants(ctx, personal.ID, []string{bob.ID}, admin.ID); !errors.Is(err, ErrTotpPrivate) {
		t.Fatalf("personal entry was assigned: %v", err)
	}

	if err := s.SetTotpGrants(ctx, shared.ID, []string{bob.ID, bob.ID}, admin.ID); err != nil {
		t.Fatal(err)
	}
	codes, err := s.TotpCodes(ctx, bob.ID)
	if err != nil || len(codes) != 1 || codes[0].Code == "" {
		t.Fatalf("bob codes: %+v %v", codes, err)
	}
	if err := s.SetTotpGrants(ctx, shared.ID, nil, admin.ID); err != nil {
		t.Fatal(err)
	}
	codes, err = s.TotpCodes(ctx, bob.ID)
	if err != nil || len(codes) != 0 {
		t.Fatalf("grant still visible: %+v %v", codes, err)
	}

	aliceCodes, err := s.TotpCodes(ctx, alice.ID)
	if err != nil || len(aliceCodes) != 1 || aliceCodes[0].Entry.ID != personal.ID {
		t.Fatalf("alice codes: %+v %v", aliceCodes, err)
	}

	var stored []byte
	if err := s.db.QueryRowContext(ctx, "SELECT secret_encrypted FROM totp_entries WHERE id = ?", shared.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("GEZD")) {
		t.Fatal("secret stored in the clear")
	}

	if _, err := s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.totpByID(ctx, personal.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("personal entry survived its owner: %v", err)
	}
	if _, err := s.totpByID(ctx, shared.ID); err != nil {
		t.Fatalf("shared entry was removed with a user: %v", err)
	}
}

func TestTotpCodeMatchesRFC(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	s.SetClock(func() time.Time { return time.Unix(59, 0) })
	u := &User{Email: "ada@example.com", PasswordHash: "x"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	e := &TotpEntry{OwnerUserID: u.ID, Name: "RFC", Digits: 8, Period: 30, Algorithm: totp.SHA1, CreatedBy: u.ID}
	if err := s.CreateTotpEntry(ctx, e, "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"); err != nil {
		t.Fatal(err)
	}
	codes, err := s.TotpCodes(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if codes[0].Code != "94287082" || codes[0].Remaining != 1 {
		t.Fatalf("%+v", codes[0])
	}
}
