package store

import (
	"context"
	"strings"
	"time"
)

// NormalizeMailbox lowercases and trims a mailbox address. Grants are always
// stored and looked up in this form.
func NormalizeMailbox(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

// MailboxGrant gives one user access to one xmail mailbox.
type MailboxGrant struct {
	Address    string
	UserID     string
	AssignedBy string
	AssignedAt time.Time
}

// UserMailboxes returns the mailboxes granted to a user, by address.
func (s *Store) UserMailboxes(ctx context.Context, userID string) ([]MailboxGrant, error) {
	return s.queryGrants(ctx, "WHERE user_id = ? ORDER BY mailbox_address", userID)
}

// AllMailboxGrants returns every grant, ordered by address then user email.
func (s *Store) AllMailboxGrants(ctx context.Context) ([]MailboxGrant, error) {
	return s.queryGrants(ctx, "ORDER BY mailbox_address, user_id")
}

// MailboxUserIDs returns the users a mailbox is granted to.
func (s *Store) MailboxUserIDs(ctx context.Context, address string) ([]string, error) {
	grants, err := s.queryGrants(ctx, "WHERE mailbox_address = ? ORDER BY user_id", NormalizeMailbox(address))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(grants))
	for _, g := range grants {
		ids = append(ids, g.UserID)
	}
	return ids, nil
}

func (s *Store) queryGrants(ctx context.Context, where string, args ...any) ([]MailboxGrant, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT mailbox_address, user_id, assigned_by, assigned_at FROM mailbox_grants "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MailboxGrant{}
	for rows.Next() {
		var g MailboxGrant
		var at int64
		if err := rows.Scan(&g.Address, &g.UserID, &g.AssignedBy, &at); err != nil {
			return nil, err
		}
		g.AssignedAt = fromUnix(at)
		out = append(out, g)
	}
	return out, rows.Err()
}

// UserHasMailbox reports whether the mailbox is granted to the user.
func (s *Store) UserHasMailbox(ctx context.Context, userID, address string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM mailbox_grants WHERE mailbox_address = ? AND user_id = ?",
		NormalizeMailbox(address), userID).Scan(&n)
	return n > 0, err
}

// SetMailboxUsers replaces the users a mailbox is granted to and returns who
// was added and who was removed. An unknown user ID returns ErrNotFound and
// changes nothing.
func (s *Store) SetMailboxUsers(ctx context.Context, address string, userIDs []string, assignedBy string) (added, removed []string, err error) {
	address = NormalizeMailbox(address)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	current := map[string]bool{}
	rows, err := tx.QueryContext(ctx, "SELECT user_id FROM mailbox_grants WHERE mailbox_address = ?", address)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		current[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	want := map[string]bool{}
	now := s.unix()
	for _, id := range userIDs {
		if want[id] {
			continue
		}
		want[id] = true
		if current[id] {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO mailbox_grants (mailbox_address, user_id, assigned_by, assigned_at) VALUES (?, ?, ?, ?)",
			address, id, assignedBy, now); err != nil {
			if strings.Contains(err.Error(), "FOREIGN KEY") {
				return nil, nil, ErrNotFound
			}
			return nil, nil, err
		}
		added = append(added, id)
	}
	for id := range current {
		if want[id] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM mailbox_grants WHERE mailbox_address = ? AND user_id = ?", address, id); err != nil {
			return nil, nil, err
		}
		removed = append(removed, id)
	}
	return added, removed, tx.Commit()
}

// DeleteMailboxGrants removes every grant of a mailbox, e.g. after it was
// deleted in xmail, and returns the users who lost access.
func (s *Store) DeleteMailboxGrants(ctx context.Context, address string) ([]string, error) {
	ids, err := s.MailboxUserIDs(ctx, address)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM mailbox_grants WHERE mailbox_address = ?", NormalizeMailbox(address))
	return ids, err
}
