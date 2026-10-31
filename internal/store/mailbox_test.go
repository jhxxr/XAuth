package store

import (
	"context"
	"errors"
	"testing"
)

func TestMailboxGrants(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	u1 := &User{Email: "alice@example.com", PasswordHash: "x"}
	u2 := &User{Email: "bob@example.com", PasswordHash: "x"}
	if err := s.CreateUser(ctx, u1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser(ctx, u2); err != nil {
		t.Fatal(err)
	}

	addr := "Info@Example.Com"
	normAddr := "info@example.com"

	// Non-existent user should fail with ErrNotFound
	_, _, err := s.SetMailboxUsers(ctx, addr, []string{"non-existent-id"}, u1.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown user, got: %v", err)
	}

	// Assign u1 to addr
	added, removed, err := s.SetMailboxUsers(ctx, addr, []string{u1.ID}, u1.ID)
	if err != nil {
		t.Fatalf("set mailbox users: %v", err)
	}
	if len(added) != 1 || added[0] != u1.ID || len(removed) != 0 {
		t.Fatalf("unexpected added/removed: +%v -%v", added, removed)
	}

	// Verify UserHasMailbox with different case
	has, err := s.UserHasMailbox(ctx, u1.ID, "INFO@EXAMPLE.COM")
	if err != nil || !has {
		t.Fatalf("UserHasMailbox u1: %v, %v", has, err)
	}
	has, err = s.UserHasMailbox(ctx, u2.ID, addr)
	if err != nil || has {
		t.Fatalf("UserHasMailbox u2: %v, %v", has, err)
	}

	// Update: add u2, keep u1
	added, removed, err = s.SetMailboxUsers(ctx, addr, []string{u1.ID, u2.ID}, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != u2.ID || len(removed) != 0 {
		t.Fatalf("expected u2 added: +%v -%v", added, removed)
	}

	userIDs, err := s.MailboxUserIDs(ctx, normAddr)
	if err != nil || len(userIDs) != 2 {
		t.Fatalf("MailboxUserIDs: %v, err=%v", userIDs, err)
	}

	// List for u2
	grants, err := s.UserMailboxes(ctx, u2.ID)
	if err != nil || len(grants) != 1 || grants[0].Address != normAddr {
		t.Fatalf("UserMailboxes for u2: %+v, err=%v", grants, err)
	}

	// Update: remove u1, keep u2
	added, removed, err = s.SetMailboxUsers(ctx, addr, []string{u2.ID}, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 || len(removed) != 1 || removed[0] != u1.ID {
		t.Fatalf("expected u1 removed: +%v -%v", added, removed)
	}

	// Delete all grants for mailbox
	deletedUsers, err := s.DeleteMailboxGrants(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(deletedUsers) != 1 || deletedUsers[0] != u2.ID {
		t.Fatalf("unexpected deleted users: %v", deletedUsers)
	}

	grants, err = s.AllMailboxGrants(ctx)
	if err != nil || len(grants) != 0 {
		t.Fatalf("expected 0 grants after delete, got %d", len(grants))
	}

	// Verify CASCADE on user deletion:
	// Re-assign u2
	_, _, _ = s.SetMailboxUsers(ctx, addr, []string{u2.ID}, u2.ID)
	// Delete user u2
	_, err = s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", u2.ID)
	if err != nil {
		t.Fatal(err)
	}
	has, err = s.UserHasMailbox(ctx, u2.ID, addr)
	if err != nil || has {
		t.Fatalf("grant should have cascaded on user delete: %v, %v", has, err)
	}
}
