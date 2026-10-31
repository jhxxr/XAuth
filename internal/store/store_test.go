package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLastAdminRule(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	// With no admin yet, ordinary edits must still work.
	u := &User{Email: "User@Example.com", PasswordHash: "x"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if u.Email != "user@example.com" {
		t.Fatalf("email not normalised: %q", u.Email)
	}
	u.Name = "Renamed"
	if err := s.UpdateUser(ctx, u); err != nil {
		t.Fatalf("edit without admins: %v", err)
	}

	a := &User{Email: "admin@example.com", PasswordHash: "x", IsAdmin: true}
	if err := s.CreateUser(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.IsAdmin = false
	if err := s.UpdateUser(ctx, a); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demote last admin: %v", err)
	}
	a.IsAdmin, a.Disabled = true, true
	if err := s.UpdateUser(ctx, a); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("disable last admin: %v", err)
	}
	if got, _ := s.UserByID(ctx, a.ID); !got.IsAdmin || got.Disabled {
		t.Fatal("rejected change was written")
	}

	if err := s.CreateUser(ctx, &User{Email: "admin@example.com", PasswordHash: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate email: %v", err)
	}
}

func TestAuthCodeSingleUse(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u := &User{Email: "a@example.com", PasswordHash: "x"}
	_ = s.CreateUser(ctx, u)
	c := &Client{Name: "App", Type: ClientWeb, RedirectURIs: []string{"https://app/cb"}}
	if _, err := s.CreateClient(ctx, c); err != nil {
		t.Fatal(err)
	}
	a := &AuthRequest{ClientID: c.ID, RedirectURI: "https://app/cb", Scopes: []string{"openid"},
		ResponseType: "code", CodeChallenge: "c", ChallengeMethod: "S256"}
	if err := s.CreateAuthRequest(ctx, a, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAuthCode(ctx, a.ID, "code", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("code issued before approval: %v", err)
	}
	if err := s.ApproveAuthRequest(ctx, a.ID, u.ID, time.Now(), []string{"pwd"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAuthCode(ctx, a.ID, "code", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAuthCode(ctx, a.ID, "code2", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second code issued: %v", err)
	}
	if _, err := s.RedeemAuthCode(ctx, "code", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemAuthCode(ctx, "code", time.Hour); !errors.Is(err, ErrCodeReused) {
		t.Fatalf("second redeem: %v", err)
	}

	// An expired code is rejected.
	b := &AuthRequest{ClientID: c.ID, RedirectURI: "https://app/cb", Scopes: []string{"openid"},
		ResponseType: "code", CodeChallenge: "c", ChallengeMethod: "S256"}
	_ = s.CreateAuthRequest(ctx, b, time.Hour)
	_ = s.ApproveAuthRequest(ctx, b.ID, u.ID, time.Now(), nil)
	_ = s.SaveAuthCode(ctx, b.ID, "late", time.Minute)
	s.SetClock(func() time.Time { return time.Now().Add(2 * time.Minute) })
	if _, err := s.RedeemAuthCode(ctx, "late", time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired code: %v", err)
	}
}
