package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

// ConsentCovers reports whether the user has already approved every requested
// scope for the client. Existing live tokens are treated as consent for
// databases created before consent records were introduced.
func (s *Store) ConsentCovers(ctx context.Context, userID, clientID string, requested []string) (bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx,
		"SELECT scopes FROM oidc_consents WHERE user_id = ? AND client_id = ?",
		userID, clientID).Scan(&raw)
	var granted []string
	if err == nil {
		if err := json.Unmarshal([]byte(raw), &granted); err != nil {
			return false, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	legacy, err := activeTokenScopes(ctx, s.db, userID, clientID, s.unix())
	if err != nil {
		return false, err
	}
	return scopeSetCovers(append(granted, legacy...), requested), nil
}

type scopeQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func activeTokenScopes(ctx context.Context, q scopeQueryer, userID, clientID string, now int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT scopes FROM access_tokens
		 WHERE user_id = ? AND client_id = ? AND revoked_at IS NULL AND expires_at > ?
		UNION ALL
		SELECT scopes FROM refresh_tokens
		 WHERE user_id = ? AND client_id = ? AND revoked_at IS NULL AND expires_at > ?`,
		userID, clientID, now, userID, clientID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var granted []string
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		var scopes []string
		if err := json.Unmarshal([]byte(encoded), &scopes); err != nil {
			return nil, err
		}
		granted = append(granted, scopes...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return granted, nil
}

// GrantConsent records the union of the scopes the user has approved for a
// client. Scope order is normalized because OAuth scopes are a set.
func (s *Store) GrantConsent(ctx context.Context, userID, clientID string, scopes []string) error {
	scopes = normalizeScopes(scopes)
	if len(scopes) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := grantConsentTx(ctx, tx, userID, clientID, scopes, s.unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func grantConsentTx(ctx context.Context, tx *sql.Tx, userID, clientID string, scopes []string, now int64) error {
	scopes = normalizeScopes(scopes)
	if len(scopes) == 0 {
		return nil
	}
	var encoded string
	err := tx.QueryRowContext(ctx,
		"SELECT scopes FROM oidc_consents WHERE user_id = ? AND client_id = ?",
		userID, clientID).Scan(&encoded)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var existing []string
		if err := json.Unmarshal([]byte(encoded), &existing); err != nil {
			return err
		}
		scopes = normalizeScopes(append(existing, scopes...))
	} else {
		legacy, err := activeTokenScopes(ctx, tx, userID, clientID, now)
		if err != nil {
			return err
		}
		scopes = normalizeScopes(append(legacy, scopes...))
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO oidc_consents (user_id, client_id, scopes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, client_id) DO UPDATE SET
			scopes = excluded.scopes,
			updated_at = excluded.updated_at`,
		userID, clientID, jsonList(scopes), now, now)
	return err
}

func scopeSetCovers(granted, requested []string) bool {
	set := make(map[string]struct{}, len(granted))
	for _, scope := range granted {
		set[scope] = struct{}{}
	}
	for _, scope := range requested {
		if _, ok := set[scope]; !ok {
			return false
		}
	}
	return true
}

func normalizeScopes(scopes []string) []string {
	set := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope != "" {
			set[scope] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for scope := range set {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}
