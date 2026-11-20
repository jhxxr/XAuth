package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Email link purposes.
const (
	EmailVerify = "verify"
	EmailReset  = "reset"
)

// EmailLinkTTL is how long a verification or reset link stays valid.
const EmailLinkTTL = 30 * time.Minute

// ErrLinkUsed is returned when a link was already consumed.
var ErrLinkUsed = errors.New("email link already used")

// CreateEmailToken stores a one-time link and returns its raw token.
// Older unused links for the same user and purpose are deleted.
func (s *Store) CreateEmailToken(ctx context.Context, userID, purpose string, ttl time.Duration) (string, error) {
	token := NewToken()
	now := s.unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM email_tokens WHERE user_id = ? AND purpose = ? AND used_at IS NULL", userID, purpose); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO email_tokens (token_hash, user_id, purpose, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		HashToken(token), userID, purpose, now, now+int64(ttl.Seconds())); err != nil {
		return "", err
	}
	return token, tx.Commit()
}

// ConsumeEmailToken marks a link used and returns the user it belongs to.
// An expired, unknown or already-used token returns ErrNotFound or ErrLinkUsed.
func (s *Store) ConsumeEmailToken(ctx context.Context, token, purpose string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var userID string
	var expires int64
	var used sql.NullInt64
	err = tx.QueryRowContext(ctx,
		"SELECT user_id, expires_at, used_at FROM email_tokens WHERE token_hash = ? AND purpose = ?",
		HashToken(token), purpose).Scan(&userID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && expires <= s.unix()) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if used.Valid {
		return "", ErrLinkUsed
	}
	if _, err := tx.ExecContext(ctx, "UPDATE email_tokens SET used_at = ? WHERE token_hash = ?", s.unix(), HashToken(token)); err != nil {
		return "", err
	}
	return userID, tx.Commit()
}

// ResetPasswordWithEmailToken atomically consumes a reset link, replaces the
// password, and revokes every existing session and token.
func (s *Store) ResetPasswordWithEmailToken(ctx context.Context, token, hash string) (*User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now, tokenHash := s.unix(), HashToken(token)
	var userID string
	var expires int64
	var used sql.NullInt64
	err = tx.QueryRowContext(ctx,
		"SELECT user_id, expires_at, used_at FROM email_tokens WHERE token_hash = ? AND purpose = ?",
		tokenHash, EmailReset).Scan(&userID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && expires <= now) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if used.Valid {
		return nil, ErrLinkUsed
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE email_tokens SET used_at = ? WHERE token_hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?",
		now, tokenHash, EmailReset, now)
	if err := affectedOne(res, err); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrLinkUsed
		}
		return nil, err
	}
	if err := setPasswordAndRevokeTx(ctx, tx, now, userID, hash, ""); err != nil {
		return nil, err
	}
	u, err := scanUser(tx.QueryRowContext(ctx, userSelect+" WHERE id = ?", userID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return u, nil
}

// SetEmailVerified marks a user's address confirmed.
func (s *Store) SetEmailVerified(ctx context.Context, userID string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET email_verified = 1, updated_at = ? WHERE id = ?", s.unix(), userID)
	return affectedOne(res, err)
}

// SetPasswordHash replaces a user's password hash.
func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?", hash, s.unix(), userID)
	return affectedOne(res, err)
}
