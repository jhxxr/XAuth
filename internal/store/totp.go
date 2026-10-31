package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"xauth/internal/totp"
)

// TOTPKeySetting is the settings row that holds the AES-256-GCM key.
const TOTPKeySetting = "totp_key"

// TotpEntry is an authenticator entry without its secret.
type TotpEntry struct {
	ID          string
	OwnerUserID string // empty for an admin-owned shared entry
	Name        string
	Issuer      string
	AccountName string
	Digits      int
	Period      int
	Algorithm   string
	Note        string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	UserIDs     []string // grants; only filled for shared entries
	Shared      bool
}

// TotpCode is a code the caller is allowed to see.
type TotpCode struct {
	Entry     TotpEntry
	Code      string
	Remaining int
}

// ErrTotpPrivate is returned when a shared-entry operation targets a personal entry.
var ErrTotpPrivate = errors.New("personal totp entry")

// TOTPKey returns the encryption key, creating it the first time.
func (s *Store) TOTPKey(ctx context.Context) ([]byte, error) {
	return s.Setting(ctx, TOTPKeySetting, 32)
}

// CreateTotpEntry stores a new entry. A non-empty owner makes it personal.
func (s *Store) CreateTotpEntry(ctx context.Context, e *TotpEntry, secret string) error {
	key, err := s.TOTPKey(ctx)
	if err != nil {
		return err
	}
	sealed, err := totp.Encrypt(key, []byte(secret))
	if err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = NewID()
	}
	now := s.unix()
	e.CreatedAt, e.UpdatedAt = fromUnix(now), fromUnix(now)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO totp_entries
		 (id, owner_user_id, name, issuer, account_name, secret_encrypted, digits, period, algorithm, note, created_by, created_at, updated_at)
		 VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.OwnerUserID, e.Name, e.Issuer, e.AccountName, sealed, e.Digits, e.Period, e.Algorithm, e.Note, e.CreatedBy, now, now)
	return err
}

// UpdateTotpEntry changes the visible fields. An empty secret keeps the stored one.
func (s *Store) UpdateTotpEntry(ctx context.Context, e *TotpEntry, secret string) error {
	var sealed []byte
	if secret != "" {
		key, err := s.TOTPKey(ctx)
		if err != nil {
			return err
		}
		sealed, err = totp.Encrypt(key, []byte(secret))
		if err != nil {
			return err
		}
	}
	now := s.unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE totp_entries SET name = ?, issuer = ?, account_name = ?, note = ?, digits = ?, period = ?, algorithm = ?,
		 secret_encrypted = CASE WHEN ? IS NULL THEN secret_encrypted ELSE ? END, updated_at = ?
		 WHERE id = ?`,
		e.Name, e.Issuer, e.AccountName, e.Note, e.Digits, e.Period, e.Algorithm, sealed, sealed, now, e.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	e.UpdatedAt = fromUnix(now)
	return nil
}

// DeleteTotpEntry removes an entry and, through the foreign key, its grants.
func (s *Store) DeleteTotpEntry(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM totp_entries WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TotpEntryByID returns one entry. Personal entries are only returned to their owner.
func (s *Store) TotpEntryByID(ctx context.Context, id, userID string, admin bool) (*TotpEntry, error) {
	e, err := s.totpByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e.OwnerUserID != "" {
		if e.OwnerUserID != userID {
			return nil, ErrNotFound
		}
		return e, nil
	}
	if admin {
		e.UserIDs, err = s.totpGrantUsers(ctx, id)
		return e, err
	}
	ok, err := s.userHasTotpGrant(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return e, nil
}

func (s *Store) totpByID(ctx context.Context, id string) (*TotpEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(owner_user_id, ''), name, issuer, account_name, digits, period, algorithm, note, created_by, created_at, updated_at
		 FROM totp_entries WHERE id = ?`, id)
	e, err := scanTotp(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// ListSharedTotp returns every admin-owned entry, with its grants.
func (s *Store) ListSharedTotp(ctx context.Context) ([]TotpEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(owner_user_id, ''), name, issuer, account_name, digits, period, algorithm, note, created_by, created_at, updated_at
		 FROM totp_entries WHERE owner_user_id IS NULL ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanTotps(rows)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Shared = true
		out[i].UserIDs, err = s.totpGrantUsers(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ListUserTotp returns the shared entries granted to the user and the user's own entries.
func (s *Store) ListUserTotp(ctx context.Context, userID string) ([]TotpEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, COALESCE(e.owner_user_id, ''), e.name, e.issuer, e.account_name, e.digits, e.period, e.algorithm, e.note, e.created_by, e.created_at, e.updated_at
		 FROM totp_entries e
		 WHERE e.owner_user_id = ?
		    OR (e.owner_user_id IS NULL AND EXISTS (SELECT 1 FROM totp_grants g WHERE g.entry_id = e.id AND g.user_id = ?))
		 ORDER BY e.owner_user_id IS NULL DESC, e.name, e.id`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanTotps(rows)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Shared = out[i].OwnerUserID == ""
	}
	return out, nil
}

// SetTotpGrants replaces who may use a shared entry. Unknown users are rejected.
func (s *Store) SetTotpGrants(ctx context.Context, entryID string, userIDs []string, assignedBy string) error {
	owner, err := s.totpOwner(ctx, entryID)
	if err != nil {
		return err
	}
	if owner != "" {
		return ErrTotpPrivate
	}
	seen := map[string]struct{}{}
	clean := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	if len(clean) > 0 {
		q := "SELECT COUNT(*) FROM users WHERE id IN (?" + strings.Repeat(", ?", len(clean)-1) + ")"
		args := make([]any, len(clean))
		for i, id := range clean {
			args[i] = id
		}
		var n int
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			return err
		}
		if n != len(clean) {
			return ErrNotFound
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM totp_grants WHERE entry_id = ?", entryID); err != nil {
		return err
	}
	now := s.unix()
	for _, id := range clean {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO totp_grants (entry_id, user_id, assigned_by, assigned_at) VALUES (?, ?, ?, ?)",
			entryID, id, assignedBy, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TotpCodes generates the current code for every entry the user may see.
func (s *Store) TotpCodes(ctx context.Context, userID string) ([]TotpCode, error) {
	entries, err := s.ListUserTotp(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.codesFor(ctx, entries)
}

// SharedTotpCodes generates codes for every shared entry. Admin only.
func (s *Store) SharedTotpCodes(ctx context.Context) ([]TotpCode, error) {
	entries, err := s.ListSharedTotp(ctx)
	if err != nil {
		return nil, err
	}
	return s.codesFor(ctx, entries)
}

func (s *Store) codesFor(ctx context.Context, entries []TotpEntry) ([]TotpCode, error) {
	if len(entries) == 0 {
		return []TotpCode{}, nil
	}
	key, err := s.TOTPKey(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]TotpCode, 0, len(entries))
	for _, e := range entries {
		var sealed []byte
		if err := s.db.QueryRowContext(ctx, "SELECT secret_encrypted FROM totp_entries WHERE id = ?", e.ID).Scan(&sealed); err != nil {
			return nil, err
		}
		plain, err := totp.Decrypt(key, sealed)
		if err != nil {
			return nil, err
		}
		code, err := totp.Code(totp.Entry{Secret: string(plain), Digits: e.Digits, Period: e.Period, Algorithm: e.Algorithm}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, TotpCode{Entry: e, Code: code, Remaining: totp.RemainingSeconds(e.Period, now)})
	}
	return out, nil
}

func (s *Store) totpOwner(ctx context.Context, id string) (string, error) {
	var owner sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT owner_user_id FROM totp_entries WHERE id = ?", id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return owner.String, err
}

func (s *Store) totpGrantUsers(ctx context.Context, entryID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT user_id FROM totp_grants WHERE entry_id = ? ORDER BY user_id", entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) userHasTotpGrant(ctx context.Context, entryID, userID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM totp_grants WHERE entry_id = ? AND user_id = ?", entryID, userID).Scan(&n)
	return n > 0, err
}

type totpScanner interface {
	Scan(dest ...any) error
}

func scanTotp(row totpScanner) (*TotpEntry, error) {
	var e TotpEntry
	var created, updated int64
	err := row.Scan(&e.ID, &e.OwnerUserID, &e.Name, &e.Issuer, &e.AccountName, &e.Digits, &e.Period, &e.Algorithm, &e.Note, &e.CreatedBy, &created, &updated)
	if err != nil {
		return nil, err
	}
	e.CreatedAt, e.UpdatedAt = fromUnix(created), fromUnix(updated)
	e.Shared = e.OwnerUserID == ""
	return &e, nil
}

func scanTotps(rows *sql.Rows) ([]TotpEntry, error) {
	out := []TotpEntry{}
	for rows.Next() {
		e, err := scanTotp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}
