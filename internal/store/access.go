package store

import (
	"context"
	"strings"
	"time"
)

// Who may sign in to an app.
const (
	AccessAll      = "all"      // every active user
	AccessAdmins   = "admins"   // admins only
	AccessSelected = "selected" // users listed in client_users
)

// UserMayUseClient reports whether u may sign in to c.
func (s *Store) UserMayUseClient(ctx context.Context, u *User, c *Client) (bool, error) {
	switch c.Access {
	case AccessAdmins:
		return u.IsAdmin, nil
	case AccessSelected:
		var n int
		err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM client_users WHERE client_id = ? AND user_id = ?", c.ID, u.ID).Scan(&n)
		return n > 0, err
	default:
		return true, nil
	}
}

// ClientUserIDs returns the users allowed to use a client in selected mode.
func (s *Store) ClientUserIDs(ctx context.Context, clientID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT cu.user_id FROM client_users cu JOIN users u ON u.id = cu.user_id WHERE cu.client_id = ? ORDER BY u.email", clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetClientUsers replaces the allow list of a client. An unknown user ID
// returns ErrNotFound and changes nothing.
func (s *Store) SetClientUsers(ctx context.Context, clientID string, userIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM client_users WHERE client_id = ?", clientID); err != nil {
		return err
	}
	for _, id := range userIDs {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO client_users (client_id, user_id) VALUES (?, ?)", clientID, id); err != nil {
			if strings.Contains(err.Error(), "FOREIGN KEY") {
				return ErrNotFound
			}
			return err
		}
	}
	return tx.Commit()
}

// disallowed selects users who may not use the client.
const disallowed = `user_id IN (SELECT u.id FROM users u, clients c WHERE c.id = ? AND (
	(c.access = 'admins' AND u.is_admin = 0) OR
	(c.access = 'selected' AND u.id NOT IN (SELECT user_id FROM client_users WHERE client_id = c.id))))`

// RevokeDisallowed revokes the tokens of users who lost access to a client.
func (s *Store) RevokeDisallowed(ctx context.Context, clientID string) error {
	now := s.unix()
	for _, table := range []string{"access_tokens", "refresh_tokens"} {
		if _, err := s.db.ExecContext(ctx,
			"UPDATE "+table+" SET revoked_at = ? WHERE client_id = ? AND revoked_at IS NULL AND "+disallowed,
			now, clientID, clientID); err != nil {
			return err
		}
	}
	return nil
}

// RevokeAdminOnlyGrants revokes a user's tokens for admins-only apps; used
// when the user stops being an admin.
func (s *Store) RevokeAdminOnlyGrants(ctx context.Context, userID string) error {
	now := s.unix()
	for _, table := range []string{"access_tokens", "refresh_tokens"} {
		if _, err := s.db.ExecContext(ctx,
			"UPDATE "+table+" SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL AND client_id IN (SELECT id FROM clients WHERE access = 'admins')",
			now, userID); err != nil {
			return err
		}
	}
	return nil
}

// Grant is an app a user has signed in to that still holds a live token.
type Grant struct {
	ClientID   string
	ClientName string
	Scopes     []string
	LastUsed   time.Time
	Refresh    bool // has an active refresh token, so it stays signed in
}

// ListGrants returns the apps that hold an active token for the user.
func (s *Store) ListGrants(ctx context.Context, userID string) ([]Grant, error) {
	now := s.unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name, t.created_at, t.scopes, t.refresh FROM clients c JOIN (
			SELECT client_id, created_at, scopes, 0 AS refresh FROM access_tokens
			 WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
			UNION ALL
			SELECT client_id, created_at, scopes, 1 FROM refresh_tokens
			 WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
		) t ON t.client_id = c.id ORDER BY c.id, t.created_at DESC`, userID, now, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		var created int64
		var scopes string
		var refresh int
		if err := rows.Scan(&g.ClientID, &g.ClientName, &created, &scopes, &refresh); err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && out[n-1].ClientID == g.ClientID {
			out[n-1].Refresh = out[n-1].Refresh || refresh == 1
			continue
		}
		g.LastUsed, g.Refresh = fromUnix(created), refresh == 1
		if err := jsonUnmarshal(scopes, &g.Scopes); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RevokeGrant revokes every token a client holds for a user.
func (s *Store) RevokeGrant(ctx context.Context, userID, clientID string) error {
	now := s.unix()
	for _, table := range []string{"access_tokens", "refresh_tokens"} {
		if _, err := s.db.ExecContext(ctx,
			"UPDATE "+table+" SET revoked_at = ? WHERE user_id = ? AND client_id = ? AND revoked_at IS NULL",
			now, userID, clientID); err != nil {
			return err
		}
	}
	return nil
}
