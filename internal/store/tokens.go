package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// AccessToken records an issued access token so it can be revoked and
// checked by the userinfo and introspection endpoints. FamilyID is the ID of
// the auth request the grant started from.
type AccessToken struct {
	ID        string
	FamilyID  string
	ClientID  string
	UserID    string
	Scopes    []string
	Audience  []string
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// RefreshToken is an opaque, rotating refresh token. Only its hash is stored.
type RefreshToken struct {
	ID        string
	FamilyID  string
	ClientID  string
	UserID    string
	Scopes    []string
	Audience  []string
	AMR       []string
	AuthTime  time.Time
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// Active reports whether the token is neither revoked nor expired.
func (t *AccessToken) Active(now time.Time) bool { return !t.Revoked && now.Before(t.ExpiresAt) }

// Active reports whether the token is neither revoked nor expired.
func (t *RefreshToken) Active(now time.Time) bool { return !t.Revoked && now.Before(t.ExpiresAt) }

// Now returns the store clock, so callers compare expiry against the same time source.
func (s *Store) Now() time.Time { return s.now() }

// IssueTokens stores a new access token and, if refresh is not nil, a new
// refresh token whose raw value is returned. When rotateFrom is set, that
// refresh token is revoked in the same transaction; if it was already revoked
// (a concurrent or repeated use), ErrTokenReused is returned and nothing is stored.
func (s *Store) IssueTokens(ctx context.Context, access *AccessToken, accessTTL time.Duration,
	refresh *RefreshToken, refreshTTL time.Duration, rotateFrom string) (string, error) {
	now := s.now().UTC().Truncate(time.Second)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if rotateFrom != "" {
		res, err := tx.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", now.Unix(), rotateFrom)
		if err := affectedOne(res, err); errors.Is(err, ErrNotFound) {
			return "", ErrTokenReused
		} else if err != nil {
			return "", err
		}
	}

	access.ID = NewID()
	access.CreatedAt, access.ExpiresAt = now, now.Add(accessTTL)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO access_tokens (id, family_id, client_id, user_id, scopes, audience, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		access.ID, access.FamilyID, access.ClientID, access.UserID, jsonList(access.Scopes), jsonList(access.Audience),
		now.Unix(), access.ExpiresAt.Unix()); err != nil {
		return "", err
	}

	var raw string
	if refresh != nil {
		raw = NewToken()
		refresh.ID = NewID()
		refresh.CreatedAt, refresh.ExpiresAt = now, now.Add(refreshTTL)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO refresh_tokens (id, token_hash, family_id, client_id, user_id, scopes, audience, amr, auth_time, created_at, expires_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			refresh.ID, HashToken(raw), refresh.FamilyID, refresh.ClientID, refresh.UserID, jsonList(refresh.Scopes),
			jsonList(refresh.Audience), jsonList(refresh.AMR), refresh.AuthTime.Unix(), now.Unix(), refresh.ExpiresAt.Unix()); err != nil {
			return "", err
		}
	}
	return raw, tx.Commit()
}

const accessCols = "id, family_id, client_id, user_id, scopes, audience, created_at, expires_at, revoked_at"

// AccessTokenByID returns an access token record whether or not it is active.
func (s *Store) AccessTokenByID(ctx context.Context, id string) (*AccessToken, error) {
	var t AccessToken
	var scopes, aud string
	var created, expires int64
	var revoked sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT "+accessCols+" FROM access_tokens WHERE id = ?", id).
		Scan(&t.ID, &t.FamilyID, &t.ClientID, &t.UserID, &scopes, &aud, &created, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(scopes), &t.Scopes); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(aud), &t.Audience); err != nil {
		return nil, err
	}
	t.CreatedAt, t.ExpiresAt, t.Revoked = fromUnix(created), fromUnix(expires), revoked.Valid
	return &t, nil
}

// RevokeAccessToken revokes one access token.
func (s *Store) RevokeAccessToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE access_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", s.unix(), id)
	return err
}

const refreshCols = "id, family_id, client_id, user_id, scopes, audience, amr, auth_time, created_at, expires_at, revoked_at"

func scanRefresh(row *sql.Row) (*RefreshToken, error) {
	var t RefreshToken
	var scopes, aud, amr string
	var authTime, created, expires int64
	var revoked sql.NullInt64
	err := row.Scan(&t.ID, &t.FamilyID, &t.ClientID, &t.UserID, &scopes, &aud, &amr, &authTime, &created, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, f := range []struct {
		src string
		dst *[]string
	}{{scopes, &t.Scopes}, {aud, &t.Audience}, {amr, &t.AMR}} {
		if err := json.Unmarshal([]byte(f.src), f.dst); err != nil {
			return nil, err
		}
	}
	t.AuthTime, t.CreatedAt, t.ExpiresAt, t.Revoked = fromUnix(authTime), fromUnix(created), fromUnix(expires), revoked.Valid
	return &t, nil
}

// RefreshTokenByRaw looks up a refresh token by its raw value, active or not.
func (s *Store) RefreshTokenByRaw(ctx context.Context, raw string) (*RefreshToken, error) {
	return scanRefresh(s.db.QueryRowContext(ctx, "SELECT "+refreshCols+" FROM refresh_tokens WHERE token_hash = ?", HashToken(raw)))
}

// RefreshTokenByID looks up a refresh token by its ID, active or not.
func (s *Store) RefreshTokenByID(ctx context.Context, id string) (*RefreshToken, error) {
	return scanRefresh(s.db.QueryRowContext(ctx, "SELECT "+refreshCols+" FROM refresh_tokens WHERE id = ?", id))
}

// RevokeFamily revokes every access and refresh token issued from one grant.
func (s *Store) RevokeFamily(ctx context.Context, familyID string) error {
	now := s.unix()
	if _, err := s.db.ExecContext(ctx, "UPDATE access_tokens SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL", now, familyID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL", now, familyID)
	return err
}

// SigningKey is a stored token signing key.
type SigningKey struct {
	ID         string
	Algorithm  string
	PrivatePEM string
	CreatedAt  time.Time
	RetiredAt  time.Time // zero while the key is published in the JWKS
}

func (s *Store) signingKeys(ctx context.Context, where string) ([]SigningKey, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, algorithm, private_pem, created_at, retired_at FROM signing_keys "+where+" ORDER BY created_at DESC, rowid DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SigningKey
	for rows.Next() {
		var k SigningKey
		var created int64
		var retired sql.NullInt64
		if err := rows.Scan(&k.ID, &k.Algorithm, &k.PrivatePEM, &created, &retired); err != nil {
			return nil, err
		}
		k.CreatedAt = fromUnix(created)
		if retired.Valid {
			k.RetiredAt = fromUnix(retired.Int64)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SigningKeys returns keys that are not retired, newest first. The first
// one signs new tokens; the others still verify tokens they signed.
func (s *Store) SigningKeys(ctx context.Context) ([]SigningKey, error) {
	return s.signingKeys(ctx, "WHERE retired_at IS NULL")
}

// AllSigningKeys returns every key including retired ones, newest first.
func (s *Store) AllSigningKeys(ctx context.Context) ([]SigningKey, error) {
	return s.signingKeys(ctx, "")
}

// RetireSigningKey removes a key from the JWKS. Tokens it signed stop verifying.
func (s *Store) RetireSigningKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE signing_keys SET retired_at = ? WHERE id = ? AND retired_at IS NULL", s.unix(), id)
	return affectedOne(res, err)
}

// CreateSigningKey stores a new signing key.
func (s *Store) CreateSigningKey(ctx context.Context, k SigningKey) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO signing_keys (id, algorithm, private_pem, created_at) VALUES (?, ?, ?, ?)",
		k.ID, k.Algorithm, k.PrivatePEM, s.unix())
	return err
}
