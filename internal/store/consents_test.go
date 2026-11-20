package store

import (
	"context"
	"testing"
	"time"
)

func TestOIDCConsentScopeCoverageAndRevocation(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u := &User{Email: "consent@example.com", PasswordHash: "x"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	c := &Client{Name: "Consent app", Type: ClientSPA}
	if _, err := s.CreateClient(ctx, c); err != nil {
		t.Fatal(err)
	}

	if covered, err := s.ConsentCovers(ctx, u.ID, c.ID, []string{"openid"}); err != nil || covered {
		t.Fatalf("unexpected initial consent: covered=%v err=%v", covered, err)
	}
	if err := s.GrantConsent(ctx, u.ID, c.ID, []string{"email", "openid"}); err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]string{{"openid"}, {"email", "openid"}} {
		if covered, err := s.ConsentCovers(ctx, u.ID, c.ID, scopes); err != nil || !covered {
			t.Errorf("consent does not cover %v: covered=%v err=%v", scopes, covered, err)
		}
	}
	if covered, err := s.ConsentCovers(ctx, u.ID, c.ID, []string{"openid", "profile"}); err != nil || covered {
		t.Fatalf("new scope unexpectedly covered: covered=%v err=%v", covered, err)
	}
	if err := s.GrantConsent(ctx, u.ID, c.ID, []string{"profile"}); err != nil {
		t.Fatal(err)
	}
	if covered, err := s.ConsentCovers(ctx, u.ID, c.ID, []string{"openid", "email", "profile"}); err != nil || !covered {
		t.Fatalf("expanded consent lost earlier scopes: covered=%v err=%v", covered, err)
	}
	grants, err := s.ListGrants(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].HasActiveTokens || !scopeSetCovers(grants[0].Scopes, []string{"openid", "email", "profile"}) {
		t.Fatalf("saved consent missing from grant list: %+v", grants)
	}
	if err := s.RevokeGrant(ctx, u.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if covered, err := s.ConsentCovers(ctx, u.ID, c.ID, []string{"openid"}); err != nil || covered {
		t.Fatalf("consent survived grant revocation: covered=%v err=%v", covered, err)
	}
	grants, err = s.ListGrants(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Fatalf("revoked consent remains in grant list: %+v", grants)
	}
}

func TestOIDCConsentRecognizesLegacyLiveTokens(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	u := &User{Email: "legacy-consent@example.com", PasswordHash: "x"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	c := &Client{Name: "Legacy app", Type: ClientSPA}
	if _, err := s.CreateClient(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IssueTokens(ctx, &AccessToken{
		FamilyID: "legacy", ClientID: c.ID, UserID: u.ID, Scopes: []string{"openid", "email"}, Audience: []string{c.ID},
	}, time.Hour, nil, 0, ""); err != nil {
		t.Fatal(err)
	}
	if covered, err := s.ConsentCovers(ctx, u.ID, c.ID, []string{"openid", "email"}); err != nil || !covered {
		t.Fatalf("legacy active token not recognized: covered=%v err=%v", covered, err)
	}
	if err := s.GrantConsent(ctx, u.ID, c.ID, []string{"openid"}); err != nil {
		t.Fatal(err)
	}
	s.SetClock(func() time.Time { return time.Now().Add(2 * time.Hour) })
	if covered, err := s.ConsentCovers(ctx, u.ID, c.ID, []string{"openid", "email"}); err != nil || !covered {
		t.Fatalf("initial consent did not preserve legacy scopes: covered=%v err=%v", covered, err)
	}
	if err := s.RevokeGrant(ctx, u.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if covered, err := s.ConsentCovers(ctx, u.ID, c.ID, []string{"openid"}); err != nil || covered {
		t.Fatalf("revoked legacy token still recognized: covered=%v err=%v", covered, err)
	}
}
