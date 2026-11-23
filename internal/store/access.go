package store

import (
	"context"
	"database/sql"
	"sort"
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

// AssignedClient is the display information for an app explicitly assigned to a user.
type AssignedClient struct {
	ID   string
	Name string
}

// ListAssignedClients lists current per-user assignments, not OAuth consents.
func (s *Store) ListAssignedClients(ctx context.Context, userID string) ([]AssignedClient, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name FROM client_users cu JOIN clients c ON c.id = cu.client_id
		WHERE cu.user_id = ? AND c.access = ? ORDER BY c.name, c.id`, userID, AccessSelected)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssignedClient{}
	for rows.Next() {
		var c AssignedClient
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClientUserIDs returns the users allowed to use a client in selected mode.
func (s *Store) ClientUserIDs(ctx context.Context, clientID string) ([]string, error) {
	return clientUserIDs(ctx, s.db, clientID)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func clientUserIDs(ctx context.Context, q queryer, clientID string) ([]string, error) {
	rows, err := q.QueryContext(ctx,
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
	if err := replaceClientUsers(ctx, tx, clientID, userIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceClientUsers(ctx context.Context, tx *sql.Tx, clientID string, userIDs []string) error {
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
	return nil
}

// disallowed selects users who may not use the client.
const disallowed = `user_id IN (SELECT u.id FROM users u, clients c WHERE c.id = ? AND (
	(c.access = 'admins' AND u.is_admin = 0) OR
	(c.access = 'selected' AND u.id NOT IN (SELECT user_id FROM client_users WHERE client_id = c.id))))`

// RevokeDisallowed revokes the tokens of users who lost access to a client.
func (s *Store) RevokeDisallowed(ctx context.Context, clientID string) error {
	now := s.unix()
	return revokeDisallowedTx(ctx, s.db, now, clientID)
}

type execQueryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func revokeDisallowedTx(ctx context.Context, q execQueryer, now int64, clientID string) error {
	for _, table := range []string{"access_tokens", "refresh_tokens"} {
		if _, err := q.ExecContext(ctx,
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := revokeAdminOnlyGrantsTx(ctx, tx, now, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func revokeAdminOnlyGrantsTx(ctx context.Context, tx *sql.Tx, now int64, userID string) error {
	for _, table := range []string{"access_tokens", "refresh_tokens"} {
		if _, err := tx.ExecContext(ctx,
			"UPDATE "+table+" SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL AND client_id IN (SELECT id FROM clients WHERE access = 'admins')",
			now, userID); err != nil {
			return err
		}
	}
	return nil
}

// Grant is an app a user has signed in to that still holds a live token.
type Grant struct {
	ClientID        string
	ClientName      string
	Scopes          []string
	LastUsed        time.Time
	Refresh         bool // has an active refresh token, so it stays signed in
	HasActiveTokens bool
}

// ListGrants returns the apps that hold an active token or a saved consent for the user.
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
	var out []Grant
	byClient := make(map[string]int)
	for rows.Next() {
		var g Grant
		var created int64
		var scopes string
		var refresh int
		if err := rows.Scan(&g.ClientID, &g.ClientName, &created, &scopes, &refresh); err != nil {
			return nil, err
		}
		if i, ok := byClient[g.ClientID]; ok {
			out[i].Refresh = out[i].Refresh || refresh == 1
			continue
		}
		g.LastUsed, g.Refresh, g.HasActiveTokens = fromUnix(created), refresh == 1, true
		if err := jsonUnmarshal(scopes, &g.Scopes); err != nil {
			return nil, err
		}
		byClient[g.ClientID] = len(out)
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	consents, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name, g.scopes, g.updated_at
		FROM oidc_consents g JOIN clients c ON c.id = g.client_id
		WHERE g.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer consents.Close()
	for consents.Next() {
		var g Grant
		var scopes string
		var updated int64
		if err := consents.Scan(&g.ClientID, &g.ClientName, &scopes, &updated); err != nil {
			return nil, err
		}
		if i, ok := byClient[g.ClientID]; ok {
			if err := jsonUnmarshal(scopes, &out[i].Scopes); err != nil {
				return nil, err
			}
			continue
		}
		g.LastUsed = fromUnix(updated)
		if err := jsonUnmarshal(scopes, &g.Scopes); err != nil {
			return nil, err
		}
		byClient[g.ClientID] = len(out)
		out = append(out, g)
	}
	if err := consents.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LastUsed.Equal(out[j].LastUsed) {
			return out[i].ClientName < out[j].ClientName
		}
		return out[i].LastUsed.After(out[j].LastUsed)
	})
	return out, nil
}

// RevokeGrant revokes every token a client holds for a user.
func (s *Store) RevokeGrant(ctx context.Context, userID, clientID string) error {
	now := s.unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"access_tokens", "refresh_tokens"} {
		if _, err := tx.ExecContext(ctx,
			"UPDATE "+table+" SET revoked_at = ? WHERE user_id = ? AND client_id = ? AND revoked_at IS NULL",
			now, userID, clientID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM oidc_consents WHERE user_id = ? AND client_id = ?", userID, clientID); err != nil {
		return err
	}
	return tx.Commit()
}
