package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// User is an account that can sign in.
type User struct {
	ID            string
	Email         string
	Name          string
	PasswordHash  string
	EmailVerified bool
	IsAdmin       bool
	Disabled      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastLoginAt   time.Time // zero if the user never signed in
}

// NormalizeEmail lowercases and trims an email address for storage and lookup.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const userCols = "id, email, name, password_hash, email_verified, is_admin, disabled, created_at, updated_at"

// userSelect also reads last_login_at, which inserts leave NULL.
const userSelect = "SELECT " + userCols + ", last_login_at FROM users"

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var verified, admin, disabled int
	var created, updated int64
	var lastLogin sql.NullInt64
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &verified, &admin, &disabled, &created, &updated, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.EmailVerified, u.IsAdmin, u.Disabled = verified == 1, admin == 1, disabled == 1
	u.CreatedAt, u.UpdatedAt = fromUnix(created), fromUnix(updated)
	if lastLogin.Valid {
		u.LastLoginAt = fromUnix(lastLogin.Int64)
	}
	return &u, nil
}

// TouchLogin records a successful sign-in.
func (s *Store) TouchLogin(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE users SET last_login_at = ? WHERE id = ?", s.unix(), userID)
	return err
}

// CreateUser inserts a user. ID, timestamps and email normalisation are filled in.
func (s *Store) CreateUser(ctx context.Context, u *User) error {
	now := s.unix()
	u.ID = NewID()
	u.Email = NormalizeEmail(u.Email)
	u.CreatedAt, u.UpdatedAt = fromUnix(now), fromUnix(now)
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO users ("+userCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		u.ID, u.Email, u.Name, u.PasswordHash, boolInt(u.EmailVerified), boolInt(u.IsAdmin), boolInt(u.Disabled), now, now)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// UserByID loads a user by ID.
func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, userSelect+" WHERE id = ?", id))
}

// UserByEmail loads a user by (normalised) email.
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, userSelect+" WHERE email = ?", NormalizeEmail(email)))
}

// ActiveUser loads a user and returns ErrNotFound if it is disabled.
func (s *Store) ActiveUser(ctx context.Context, id string) (*User, error) {
	u, err := s.UserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, ErrNotFound
	}
	return u, nil
}

// ListUsers returns all users, newest first.
func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, userSelect+" ORDER BY created_at DESC, email")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountAdmins returns the number of active admins.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled = 0").Scan(&n)
	return n, err
}

// UpdateUser writes name, password hash and flags. It refuses to leave the
// system without an active admin.
func (s *Store) UpdateUser(ctx context.Context, u *User) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const countAdmins = "SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled = 0"
	var before, after int
	if err := tx.QueryRowContext(ctx, countAdmins).Scan(&before); err != nil {
		return err
	}
	now := s.unix()
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET name = ?, password_hash = ?, email_verified = ?, is_admin = ?, disabled = ?, updated_at = ?
		 WHERE id = ?`,
		u.Name, u.PasswordHash, boolInt(u.EmailVerified), boolInt(u.IsAdmin), boolInt(u.Disabled), now, u.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := tx.QueryRowContext(ctx, countAdmins).Scan(&after); err != nil {
		return err
	}
	if before > 0 && after == 0 {
		return ErrLastAdmin
	}
	u.UpdatedAt = fromUnix(now)
	return tx.Commit()
}

// RevokeUserAccess deletes all sessions of a user, optionally keeping one,
// and revokes all of the user's tokens.
func (s *Store) RevokeUserAccess(ctx context.Context, userID, keepSessionHash string) error {
	now := s.unix()
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?", userID, keepSessionHash); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE access_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL", now, userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL", now, userID)
	return err
}
