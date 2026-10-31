package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"
)

// Session is a signed-in browser. The raw token lives only in the cookie;
// ID is a separate public handle used to list and revoke devices.
type Session struct {
	ID        string
	TokenHash string
	UserID    string
	UserAgent string
	IP        string
	AuthTime  time.Time
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
}

// SessionMeta describes the browser that signed in.
type SessionMeta struct {
	UserAgent string
	IP        string
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// CreateSession starts a session and returns the raw cookie token.
func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration, meta SessionMeta) (string, *Session, error) {
	token := NewToken()
	now := s.now().UTC().Truncate(time.Second)
	sess := &Session{
		ID: NewID(), TokenHash: HashToken(token), UserID: userID,
		UserAgent: truncate(meta.UserAgent, 256), IP: truncate(meta.IP, 64),
		AuthTime: now, CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(ttl),
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, token_hash, user_id, user_agent, ip, auth_time, created_at, last_seen, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.TokenHash, userID, sess.UserAgent, sess.IP, now.Unix(), now.Unix(), now.Unix(), sess.ExpiresAt.Unix())
	if err != nil {
		return "", nil, err
	}
	return token, sess, nil
}

const sessionCols = "id, token_hash, user_id, user_agent, ip, auth_time, created_at, last_seen, expires_at"

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	var sess Session
	var authTime, created, lastSeen, expires int64
	err := row.Scan(&sess.ID, &sess.TokenHash, &sess.UserID, &sess.UserAgent, &sess.IP, &authTime, &created, &lastSeen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.AuthTime, sess.CreatedAt = fromUnix(authTime), fromUnix(created)
	sess.LastSeen, sess.ExpiresAt = fromUnix(lastSeen), fromUnix(expires)
	return &sess, nil
}

// SessionByToken returns the unexpired session for a raw cookie token.
func (s *Store) SessionByToken(ctx context.Context, token string) (*Session, error) {
	return scanSession(s.db.QueryRowContext(ctx,
		"SELECT "+sessionCols+" FROM sessions WHERE token_hash = ? AND expires_at > ?", HashToken(token), s.unix()))
}

// ListSessions returns a user's unexpired sessions, most recently used first.
func (s *Store) ListSessions(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+sessionCols+" FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY last_seen DESC, created_at DESC",
		userID, s.unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// TouchSession slides the expiry forward. Callers throttle how often they call it.
func (s *Store) TouchSession(ctx context.Context, tokenHash string, ttl time.Duration) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET last_seen = ?, expires_at = ? WHERE token_hash = ?",
		now.Unix(), now.Add(ttl).Unix(), tokenHash)
	return err
}

// DeleteSession ends one session by its token hash.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

// DeleteUserSession ends one of a user's sessions by its public ID.
func (s *Store) DeleteUserSession(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND id = ?", userID, id)
	return affectedOne(res, err)
}

// DeleteOtherSessions ends every session of a user except keepHash and
// returns how many were ended.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID, keepHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?", userID, keepHash)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
