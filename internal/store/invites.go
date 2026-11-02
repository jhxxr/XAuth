package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"
)

// Registration invite errors.
var (
	// ErrInviteInvalid means the code is unknown, revoked or already used.
	ErrInviteInvalid = errors.New("invite is invalid or already used")
	// ErrInviteExpired means the code existed but is past its expiry.
	ErrInviteExpired = errors.New("invite has expired")
	// ErrInviteEmail means the code is reserved for another address.
	ErrInviteEmail = errors.New("invite is for another email address")
)

// Invite is a one-time code that lets somebody create an account while
// self-service registration is closed. Only the hash of the code is stored.
type Invite struct {
	ID          string
	Email       string // empty means any address may use it
	Note        string
	CreatedBy   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	UsedBy      string
	UsedByEmail string // filled by ListInvites
	UsedAt      time.Time
	RevokedAt   time.Time
}

// inviteAlphabet leaves out I, O, 0 and 1 so codes are hard to misread.
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewInviteCode returns a human-friendly code such as K7QF-9M2X-PW3D.
// It carries 60 bits of entropy; the registration endpoint is rate limited.
func NewInviteCode() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	var sb strings.Builder
	for i, v := range b {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		// len(inviteAlphabet) is 32, so the byte maps without bias.
		sb.WriteByte(inviteAlphabet[int(v)%len(inviteAlphabet)])
	}
	return sb.String()
}

// NormalizeInviteCode ignores case, dashes and spaces, so a code can be
// retyped from a message without worrying about formatting.
func NormalizeInviteCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if r == '-' || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

const inviteCols = "id, email, note, created_by, created_at, expires_at, used_by, used_at, revoked_at"

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	var inv Invite
	var created, expires int64
	var usedAt, revokedAt sql.NullInt64
	err := row.Scan(&inv.ID, &inv.Email, &inv.Note, &inv.CreatedBy, &created, &expires, &inv.UsedBy, &usedAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	inv.CreatedAt, inv.ExpiresAt = fromUnix(created), fromUnix(expires)
	if usedAt.Valid {
		inv.UsedAt = fromUnix(usedAt.Int64)
	}
	if revokedAt.Valid {
		inv.RevokedAt = fromUnix(revokedAt.Int64)
	}
	return &inv, nil
}

// CreateInvite stores an invite for code. ID and CreatedAt are filled in.
func (s *Store) CreateInvite(ctx context.Context, inv *Invite, code string) error {
	now := s.now()
	inv.ID = NewID()
	inv.Email = NormalizeEmail(inv.Email)
	inv.CreatedAt = now.UTC()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO invites (id, code_hash, email, note, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		inv.ID, HashToken(NormalizeInviteCode(code)), inv.Email, inv.Note, inv.CreatedBy, now.Unix(), inv.ExpiresAt.Unix())
	return err
}

// InviteByCode looks a code up by its hash.
func (s *Store) InviteByCode(ctx context.Context, code string) (*Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx,
		"SELECT "+inviteCols+" FROM invites WHERE code_hash = ?", HashToken(NormalizeInviteCode(code))))
}

// ListInvites returns invites newest first. Only hashes are kept, so the
// codes themselves cannot be shown again.
func (s *Store) ListInvites(ctx context.Context, limit int) ([]*Invite, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.email, i.note, i.created_by, i.created_at, i.expires_at, i.used_by, i.used_at, i.revoked_at,
		        COALESCE(u.email, '')
		 FROM invites i LEFT JOIN users u ON u.id = i.used_by
		 ORDER BY i.created_at DESC, i.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Invite
	for rows.Next() {
		var inv Invite
		var created, expires int64
		var usedAt, revokedAt sql.NullInt64
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Note, &inv.CreatedBy, &created, &expires,
			&inv.UsedBy, &usedAt, &revokedAt, &inv.UsedByEmail); err != nil {
			return nil, err
		}
		inv.CreatedAt, inv.ExpiresAt = fromUnix(created), fromUnix(expires)
		if usedAt.Valid {
			inv.UsedAt = fromUnix(usedAt.Int64)
		}
		if revokedAt.Valid {
			inv.RevokedAt = fromUnix(revokedAt.Int64)
		}
		out = append(out, &inv)
	}
	return out, rows.Err()
}

// RevokeInvite disables a pending invite.
func (s *Store) RevokeInvite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE invites SET revoked_at = ? WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL", s.unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var exists int
	err = s.db.QueryRowContext(ctx, "SELECT 1 FROM invites WHERE id = ?", id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrInviteInvalid
}

// RedeemInvite validates code and creates the user and the "used" mark in
// one transaction, so a code can never be spent twice.
func (s *Store) RedeemInvite(ctx context.Context, code string, u *User) (*Invite, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.unix()
	inv, err := scanInvite(tx.QueryRowContext(ctx,
		"SELECT "+inviteCols+" FROM invites WHERE code_hash = ?", HashToken(NormalizeInviteCode(code))))
	if err != nil {
		return nil, err
	}
	switch {
	case !inv.RevokedAt.IsZero() || !inv.UsedAt.IsZero():
		return nil, ErrInviteInvalid
	case inv.ExpiresAt.Unix() <= now:
		return nil, ErrInviteExpired
	case inv.Email != "" && inv.Email != NormalizeEmail(u.Email):
		return nil, ErrInviteEmail
	}
	if err := insertUser(ctx, tx, u, now); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE invites SET used_by = ?, used_at = ? WHERE id = ?", u.ID, now, inv.ID); err != nil {
		return nil, err
	}
	inv.UsedBy, inv.UsedAt = u.ID, fromUnix(now)
	return inv, tx.Commit()
}
