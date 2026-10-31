package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Auth request status values.
const (
	AuthPending  = "pending"  // waiting for the user to sign in
	AuthApproved = "approved" // user signed in; a code may be issued
	AuthDenied   = "denied"   // user cancelled
	AuthRedeemed = "redeemed" // code exchanged; kept to detect code reuse
)

// AuthRequest is an in-progress OIDC authorization.
type AuthRequest struct {
	ID              string
	ClientID        string
	RedirectURI     string
	Scopes          []string
	State           string
	Nonce           string
	ResponseType    string
	ResponseMode    string
	CodeChallenge   string
	ChallengeMethod string
	Prompt          []string
	MaxAge          *uint
	LoginHint       string
	HintSubject     string
	Status          string
	UserID          string
	AuthTime        time.Time
	AMR             []string
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

const authCols = `id, client_id, redirect_uri, scopes, state, nonce, response_type, response_mode,
	code_challenge, challenge_method, prompt, max_age, login_hint, hint_subject, status,
	user_id, auth_time, amr, created_at, expires_at`

func scanAuthRequest(row interface{ Scan(...any) error }) (*AuthRequest, error) {
	var a AuthRequest
	var scopes, prompt, amr string
	var maxAge sql.NullInt64
	var userID sql.NullString
	var authTime sql.NullInt64
	var created, expires int64
	err := row.Scan(&a.ID, &a.ClientID, &a.RedirectURI, &scopes, &a.State, &a.Nonce, &a.ResponseType, &a.ResponseMode,
		&a.CodeChallenge, &a.ChallengeMethod, &prompt, &maxAge, &a.LoginHint, &a.HintSubject, &a.Status,
		&userID, &authTime, &amr, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, f := range []struct {
		src string
		dst *[]string
	}{{scopes, &a.Scopes}, {prompt, &a.Prompt}, {amr, &a.AMR}} {
		if err := json.Unmarshal([]byte(f.src), f.dst); err != nil {
			return nil, err
		}
	}
	if maxAge.Valid {
		v := uint(maxAge.Int64)
		a.MaxAge = &v
	}
	a.UserID = userID.String
	if authTime.Valid {
		a.AuthTime = fromUnix(authTime.Int64)
	}
	a.CreatedAt, a.ExpiresAt = fromUnix(created), fromUnix(expires)
	return &a, nil
}

// CreateAuthRequest stores a new pending request valid for ttl.
func (s *Store) CreateAuthRequest(ctx context.Context, a *AuthRequest, ttl time.Duration) error {
	now := s.now()
	a.ID = NewID()
	a.Status = AuthPending
	a.CreatedAt = now.UTC().Truncate(time.Second)
	a.ExpiresAt = now.Add(ttl).UTC().Truncate(time.Second)
	var maxAge any
	if a.MaxAge != nil {
		maxAge = int64(*a.MaxAge)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_requests (id, client_id, redirect_uri, scopes, state, nonce, response_type, response_mode,
			code_challenge, challenge_method, prompt, max_age, login_hint, hint_subject, status, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ClientID, a.RedirectURI, jsonList(a.Scopes), a.State, a.Nonce, a.ResponseType, a.ResponseMode,
		a.CodeChallenge, a.ChallengeMethod, jsonList(a.Prompt), maxAge, a.LoginHint, a.HintSubject, a.Status,
		toUnix(a.CreatedAt), toUnix(a.ExpiresAt))
	return err
}

// AuthRequestByID returns an unexpired request in any status.
func (s *Store) AuthRequestByID(ctx context.Context, id string) (*AuthRequest, error) {
	return scanAuthRequest(s.db.QueryRowContext(ctx,
		"SELECT "+authCols+" FROM auth_requests WHERE id = ? AND expires_at > ?", id, s.unix()))
}

// ApproveAuthRequest records who signed in. Only pending requests can be approved.
func (s *Store) ApproveAuthRequest(ctx context.Context, id, userID string, authTime time.Time, amr []string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE auth_requests SET status = ?, user_id = ?, auth_time = ?, amr = ?
		 WHERE id = ? AND status = ? AND expires_at > ?`,
		AuthApproved, userID, authTime.Unix(), jsonList(amr), id, AuthPending, s.unix())
	return affectedOne(res, err)
}

// DenyAuthRequest marks a pending request as cancelled by the user.
func (s *Store) DenyAuthRequest(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE auth_requests SET status = ? WHERE id = ? AND status = ? AND expires_at > ?",
		AuthDenied, id, AuthPending, s.unix())
	return affectedOne(res, err)
}

// SaveAuthCode attaches the hash of an issued code. Each request gets at most
// one code, and the code is only valid for ttl.
func (s *Store) SaveAuthCode(ctx context.Context, id, code string, ttl time.Duration) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE auth_requests SET code_hash = ?, expires_at = ?
		 WHERE id = ? AND status = ? AND code_hash IS NULL AND expires_at > ?`,
		HashToken(code), s.now().Add(ttl).Unix(), id, AuthApproved, s.unix())
	return affectedOne(res, err)
}

// RedeemAuthCode atomically marks a code as used and returns its request.
// The row is kept for keep so that a second use can be detected; on reuse
// every token issued from the first use is revoked and ErrCodeReused returned.
func (s *Store) RedeemAuthCode(ctx context.Context, code string, keep time.Duration) (*AuthRequest, error) {
	hash := HashToken(code)
	now := s.unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE auth_requests SET status = ?, expires_at = ?
		 WHERE code_hash = ? AND status = ? AND expires_at > ?`,
		AuthRedeemed, s.now().Add(keep).Unix(), hash, AuthApproved, now)
	if err := affectedOne(res, err); err == nil {
		return scanAuthRequest(s.db.QueryRowContext(ctx, "SELECT "+authCols+" FROM auth_requests WHERE code_hash = ?", hash))
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	var id, status, clientID string
	var userID sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT id, status, client_id, user_id FROM auth_requests WHERE code_hash = ?", hash).
		Scan(&id, &status, &clientID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status == AuthRedeemed {
		if err := s.RevokeFamily(ctx, id); err != nil {
			return nil, err
		}
		_ = s.Record(ctx, Event{Type: EvTokenReuse, ActorID: userID.String, SubjectType: "app", SubjectID: clientID,
			Detail: map[string]any{"kind": "code"}})
		return nil, ErrCodeReused
	}
	return nil, ErrNotFound
}

// DeleteAuthRequest removes a finished request. Redeemed requests are kept
// until they expire so code reuse stays detectable.
func (s *Store) DeleteAuthRequest(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM auth_requests WHERE id = ? AND status <> ?", id, AuthRedeemed)
	return err
}

func affectedOne(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
