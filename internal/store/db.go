// Package store persists XAuth state in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var (
	// ErrNotFound is returned when a row does not exist or has expired.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned on a unique constraint violation.
	ErrConflict = errors.New("already exists")
	// ErrLastAdmin is returned when a change would leave no active admin.
	ErrLastAdmin = errors.New("at least one active admin is required")
	// ErrCodeReused is returned when an authorization code is presented twice.
	ErrCodeReused = errors.New("authorization code already used")
	// ErrTokenReused is returned when a rotated refresh token is presented again.
	ErrTokenReused = errors.New("refresh token already used")
)

// Store wraps the SQLite database.
type Store struct {
	db    *sql.DB
	clock atomic.Pointer[func() time.Time]
}

// AuditRetention is how long audit events are kept.
const AuditRetention = 180 * 24 * time.Hour

// Open opens (and creates if needed) the database file and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db}
	s.SetClock(time.Now)
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping checks the database connection.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// SetClock replaces the time source; tests use it to move time forward.
// It is safe to call while requests are being served.
func (s *Store) SetClock(now func() time.Time) { s.clock.Store(&now) }

func (s *Store) now() time.Time { return (*s.clock.Load())() }

func (s *Store) unix() int64 { return s.now().Unix() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Setting returns a persistent random secret, creating it on first use.
func (s *Store) Setting(ctx context.Context, key string, size int) ([]byte, error) {
	var v []byte
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	v = make([]byte, size)
	if _, err := rand.Read(v); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING", key, v); err != nil {
		return nil, err
	}
	// Re-read in case another process inserted first.
	err = s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	return v, err
}

// Purge deletes expired rows. Revoked refresh tokens are kept until they
// expire so that reuse of a rotated token can still be detected.
func (s *Store) Purge(ctx context.Context) error {
	now := s.unix()
	stmts := []string{
		"DELETE FROM auth_requests WHERE expires_at < ?",
		"DELETE FROM sessions WHERE expires_at < ?",
		"DELETE FROM access_tokens WHERE expires_at < ?",
		"DELETE FROM refresh_tokens WHERE expires_at < ?",
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q, now); err != nil {
			return err
		}
	}
	// Unused invites stay visible to admins for a month after expiry.
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM invites WHERE used_at IS NULL AND expires_at < ?",
		now-int64((30*24*time.Hour)/time.Second)); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM audit_events WHERE at < ?", now-int64(AuditRetention.Seconds()))
	return err
}

// NewID returns a random 128-bit identifier as 32 hex characters.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// NewToken returns a random 256-bit secret, base64url encoded.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken returns the SHA-256 of a high-entropy secret. Only hashes of
// session, code, refresh token and client secret values are stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// EqualHash compares a secret against a stored hash in constant time.
func EqualHash(secret, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(HashToken(secret)), []byte(hash)) == 1
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func fromUnix(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0).UTC()
}

func toUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
