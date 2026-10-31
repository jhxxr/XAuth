package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MaxAvatarBytes caps the decoded size of a profile picture. The web UI
// shrinks pictures well below this; the limit only guards the API.
const MaxAvatarBytes = 1 << 20

// Avatar is a user's profile picture together with its cache version.
type Avatar struct {
	UserID    string
	MIME      string
	Data      []byte
	UpdatedAt time.Time
}

// SetAvatar stores a new picture for a user and bumps the cache version.
// The returned time is the new version.
func (s *Store) SetAvatar(ctx context.Context, userID, mime string, data []byte) (time.Time, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()
	now := s.unix()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO user_avatars (user_id, mime, data, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET mime = excluded.mime, data = excluded.data, updated_at = excluded.updated_at`,
		userID, mime, data, now)
	if err != nil {
		return time.Time{}, err
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE users SET avatar_updated_at = ?, updated_at = ? WHERE id = ?", now, now, userID)
	if err != nil {
		return time.Time{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return time.Time{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return fromUnix(now), nil
}

// DeleteAvatar removes a user's picture. Removing the picture of a user who
// has none is not an error, but an unknown user is ErrNotFound.
func (s *Store) DeleteAvatar(ctx context.Context, userID string) (time.Time, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()
	now := s.unix()
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_avatars WHERE user_id = ?", userID); err != nil {
		return time.Time{}, err
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE users SET avatar_updated_at = NULL, updated_at = ? WHERE id = ?", now, userID)
	if err != nil {
		return time.Time{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return time.Time{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return fromUnix(now), nil
}

// Avatar loads a user's picture. It returns ErrNotFound when there is none.
func (s *Store) Avatar(ctx context.Context, userID string) (*Avatar, error) {
	var a Avatar
	var updated int64
	err := s.db.QueryRowContext(ctx,
		"SELECT user_id, mime, data, updated_at FROM user_avatars WHERE user_id = ?", userID).
		Scan(&a.UserID, &a.MIME, &a.Data, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.UpdatedAt = fromUnix(updated)
	return &a, nil
}
