package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// WebAuthnCredential is one stored passkey.
type WebAuthnCredential struct {
	ID         string
	UserID     string
	Name       string
	Data       webauthn.Credential
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// AddWebAuthnCredential stores a credential for a user.
func (s *Store) AddWebAuthnCredential(ctx context.Context, userID, name string, cred *webauthn.Credential) (*WebAuthnCredential, error) {
	id := NewID()
	data, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	now := s.unix()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO webauthn_credentials (id, user_id, name, credential, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, userID, truncate(name, 64), string(data), now); err != nil {
		return nil, err
	}
	return &WebAuthnCredential{ID: id, UserID: userID, Name: name, Data: *cred, CreatedAt: fromUnix(now)}, nil
}

// UserWebAuthnCredentials returns a user's passkeys, oldest first.
func (s *Store) UserWebAuthnCredentials(ctx context.Context, userID string) ([]WebAuthnCredential, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, user_id, name, credential, created_at, last_used_at FROM webauthn_credentials WHERE user_id = ? ORDER BY created_at",
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WebAuthnCredential{}
	for rows.Next() {
		var c WebAuthnCredential
		var raw string
		var created int64
		var used sql.NullInt64
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &raw, &created, &used); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c.Data); err != nil {
			return nil, err
		}
		c.CreatedAt = fromUnix(created)
		if used.Valid {
			c.LastUsedAt = fromUnix(used.Int64)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FindWebAuthnCredential looks a credential up by its raw credential ID.
func (s *Store) FindWebAuthnCredential(ctx context.Context, rawID []byte) (*WebAuthnCredential, *User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, user_id, name, credential, created_at, last_used_at FROM webauthn_credentials")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c WebAuthnCredential
		var raw string
		var created int64
		var used sql.NullInt64
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &raw, &created, &used); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c.Data); err != nil {
			return nil, nil, err
		}
		if !equalBytes(c.Data.ID, rawID) {
			continue
		}
		u, err := s.UserByID(ctx, c.UserID)
		if err != nil {
			return nil, nil, err
		}
		if used.Valid {
			c.LastUsedAt = fromUnix(used.Int64)
		}
		c.CreatedAt = fromUnix(created)
		return &c, u, nil
	}
	return nil, nil, ErrNotFound
}

// UpdateWebAuthnCredential writes back sign counter and flags after a login.
func (s *Store) UpdateWebAuthnCredential(ctx context.Context, rowID string, cred *webauthn.Credential) error {
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE webauthn_credentials SET credential = ?, last_used_at = ? WHERE id = ?",
		string(data), s.unix(), rowID)
	return affectedOne(res, err)
}

// RenameWebAuthnCredential changes the label shown in the account page.
func (s *Store) RenameWebAuthnCredential(ctx context.Context, userID, id, name string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE webauthn_credentials SET name = ? WHERE id = ? AND user_id = ?", truncate(name, 64), id, userID)
	return affectedOne(res, err)
}

// DeleteWebAuthnCredential removes one of a user's passkeys.
func (s *Store) DeleteWebAuthnCredential(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM webauthn_credentials WHERE id = ? AND user_id = ?", id, userID)
	return affectedOne(res, err)
}

// SaveWebAuthnChallenge keeps ceremony state between begin and finish.
// The row is single-use and short-lived.
func (s *Store) SaveWebAuthnChallenge(ctx context.Context, id, session string, ttl time.Duration) error {
	now := s.unix()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO webauthn_challenges (id, session, created_at, expires_at) VALUES (?, ?, ?, ?)",
		id, session, now, now+int64(ttl.Seconds()))
	return err
}

// TakeWebAuthnChallenge returns and deletes the ceremony state.
func (s *Store) TakeWebAuthnChallenge(ctx context.Context, id string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var session string
	var expires int64
	err = tx.QueryRowContext(ctx, "SELECT session, expires_at FROM webauthn_challenges WHERE id = ?", id).Scan(&session, &expires)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && expires <= s.unix()) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM webauthn_challenges WHERE id = ?", id); err != nil {
		return "", err
	}
	return session, tx.Commit()
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// WebAuthnUser adapts a User and its credentials to the library's interface.
type WebAuthnUser struct {
	User        *User
	Credentials []WebAuthnCredential
	// Current is the row matched during a discoverable login, so the caller
	// can write back the sign counter to the right record.
	Current *WebAuthnCredential
}

// CurrentRowID is the storage id of the credential used to log in.
func (u WebAuthnUser) CurrentRowID() string {
	if u.Current == nil {
		return ""
	}
	return u.Current.ID
}

// WebAuthnID returns the user handle. The XAuth id is 32 hex characters, well
// under WebAuthn's 64-byte limit, and is stable for the account's lifetime.
func (u WebAuthnUser) WebAuthnID() []byte { return []byte(u.User.ID) }

// WebAuthnName is the account name shown by the authenticator.
func (u WebAuthnUser) WebAuthnName() string { return u.User.Email }

// WebAuthnDisplayName is the friendly label.
func (u WebAuthnUser) WebAuthnDisplayName() string {
	if u.User.Name != "" {
		return u.User.Name
	}
	return u.User.Email
}

// WebAuthnCredentials returns the stored credentials.
func (u WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(u.Credentials))
	for _, c := range u.Credentials {
		out = append(out, c.Data)
	}
	return out
}
