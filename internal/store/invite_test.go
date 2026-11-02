package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInviteLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	code := NewInviteCode()
	if len(code) != 14 {
		t.Fatalf("unexpected code shape %q", code)
	}
	inv := &Invite{Email: "Friend@Example.com", Note: "hello", CreatedBy: "admin", ExpiresAt: s.now().Add(time.Hour)}
	if err := s.CreateInvite(ctx, inv, code); err != nil {
		t.Fatal(err)
	}
	if inv.Email != "friend@example.com" || inv.ID == "" || inv.CreatedAt.IsZero() {
		t.Fatalf("invite not normalised: %+v", inv)
	}

	// Formatting must not matter when a code is typed in.
	got, err := s.InviteByCode(ctx, strings.ToLower(strings.ReplaceAll(code, "-", " ")))
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.ID != inv.ID || got.Email != inv.Email || got.Note != "hello" {
		t.Fatalf("lookup returned %+v", got)
	}

	if err := s.RevokeInvite(ctx, inv.ID); err != nil {
		t.Fatal(err)
	}
	revoked, _ := s.InviteByCode(ctx, code)
	if revoked.RevokedAt.IsZero() {
		t.Fatal("revoke did not set revoked_at")
	}
	if _, err := s.RedeemInvite(ctx, code, &User{Email: "friend@example.com", PasswordHash: "x"}); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("revoked invite redeemed: %v", err)
	}
	if err := s.RevokeInvite(ctx, inv.ID); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("second revoke: %v", err)
	}
	if err := s.RevokeInvite(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown invite: %v", err)
	}
}

func TestInviteRedeemIsSingleUse(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	code := NewInviteCode()
	inv := &Invite{Email: "friend@example.com", ExpiresAt: s.now().Add(time.Hour)}
	if err := s.CreateInvite(ctx, inv, code); err != nil {
		t.Fatal(err)
	}
	u := &User{Email: "friend@example.com", PasswordHash: "x", EmailVerified: true}
	redeemed, err := s.RedeemInvite(ctx, code, u)
	if err != nil {
		t.Fatal(err)
	}
	if redeemed.UsedBy != u.ID || redeemed.UsedAt.IsZero() {
		t.Fatalf("invite not marked used: %+v", redeemed)
	}
	stored, err := s.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Email != "friend@example.com" || !stored.EmailVerified {
		t.Fatalf("user created with %+v", stored)
	}
	if _, err := s.RedeemInvite(ctx, code, &User{Email: "other@example.com", PasswordHash: "x"}); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("second redeem: %v", err)
	}
}

func TestInviteExpiryAndEmailBinding(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	code := NewInviteCode()
	inv := &Invite{Email: "owner@example.com", ExpiresAt: s.now().Add(time.Hour)}
	if err := s.CreateInvite(ctx, inv, code); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemInvite(ctx, code, &User{Email: "other@example.com", PasswordHash: "x"}); !errors.Is(err, ErrInviteEmail) {
		t.Fatalf("mismatched email: %v", err)
	}
	// The failed attempt must not consume the invite.
	s.SetClock(func() time.Time { return time.Now().Add(2 * time.Hour) })
	if _, err := s.RedeemInvite(ctx, code, &User{Email: "owner@example.com", PasswordHash: "x"}); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired invite: %v", err)
	}
}

func TestInviteSurvivesEmailConflict(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	taken := &User{Email: "taken@example.com", PasswordHash: "x"}
	if err := s.CreateUser(ctx, taken); err != nil {
		t.Fatal(err)
	}
	code := NewInviteCode()
	if err := s.CreateInvite(ctx, &Invite{ExpiresAt: s.now().Add(time.Hour)}, code); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemInvite(ctx, code, &User{Email: "taken@example.com", PasswordHash: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate email: %v", err)
	}
	// The failed transaction must leave the code usable.
	if _, err := s.RedeemInvite(ctx, code, &User{Email: "fresh@example.com", PasswordHash: "x"}); err != nil {
		t.Fatalf("redeem after conflict: %v", err)
	}
}

func TestRegistrationSettings(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	def := RegistrationSettings{Mode: RegistrationInvite}
	got, err := s.Registration(ctx, def)
	if err != nil || got != def {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	want := RegistrationSettings{Mode: RegistrationOpen, InviteVerifyEmail: true}
	if err := s.SetRegistration(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err = s.Registration(ctx, def)
	if err != nil || got != want {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}
