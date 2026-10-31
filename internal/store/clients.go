package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Client types. Only web clients are confidential and hold a secret.
const (
	ClientWeb    = "web"
	ClientSPA    = "spa"
	ClientNative = "native"
)

// Client is an application registered to sign users in through XAuth.
type Client struct {
	ID                     string
	Name                   string
	Type                   string
	SecretHash             string
	RedirectURIs           []string
	PostLogoutRedirectURIs []string
	AllowRefresh           bool
	Access                 string // AccessAll, AccessAdmins or AccessSelected
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// Confidential reports whether the client authenticates with a secret.
func (c *Client) Confidential() bool { return c.Type == ClientWeb }

const clientCols = "id, name, type, secret_hash, redirect_uris, post_logout_redirect_uris, allow_refresh, access, created_at, updated_at"

func scanClient(row interface{ Scan(...any) error }) (*Client, error) {
	var c Client
	var redirects, logouts string
	var refresh int
	var created, updated int64
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.SecretHash, &redirects, &logouts, &refresh, &c.Access, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(redirects), &c.RedirectURIs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(logouts), &c.PostLogoutRedirectURIs); err != nil {
		return nil, err
	}
	c.AllowRefresh = refresh == 1
	c.CreatedAt, c.UpdatedAt = fromUnix(created), fromUnix(updated)
	return &c, nil
}

func jsonList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// CreateClient inserts a client. For web clients it generates a secret and
// returns it; the secret cannot be read back later.
func (s *Store) CreateClient(ctx context.Context, c *Client) (secret string, err error) {
	now := s.unix()
	c.ID = NewID()
	c.CreatedAt, c.UpdatedAt = fromUnix(now), fromUnix(now)
	c.SecretHash = ""
	if c.Access == "" {
		c.Access = AccessAll
	}
	if c.Confidential() {
		secret = NewToken()
		c.SecretHash = HashToken(secret)
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO clients ("+clientCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		c.ID, c.Name, c.Type, c.SecretHash, jsonList(c.RedirectURIs), jsonList(c.PostLogoutRedirectURIs),
		boolInt(c.AllowRefresh), c.Access, now, now)
	if err != nil {
		return "", err
	}
	return secret, nil
}

// ClientByID loads a client.
func (s *Store) ClientByID(ctx context.Context, id string) (*Client, error) {
	return scanClient(s.db.QueryRowContext(ctx, "SELECT "+clientCols+" FROM clients WHERE id = ?", id))
}

// ListClients returns all clients ordered by name.
func (s *Store) ListClients(ctx context.Context) ([]*Client, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+clientCols+" FROM clients ORDER BY name COLLATE NOCASE, created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateClient writes the editable fields. The type and secret are unchanged.
// Turning off refresh tokens revokes the ones already issued. Callers that
// narrow Access must also call RevokeDisallowed.
func (s *Store) UpdateClient(ctx context.Context, c *Client) error {
	now := s.unix()
	res, err := s.db.ExecContext(ctx,
		"UPDATE clients SET name = ?, redirect_uris = ?, post_logout_redirect_uris = ?, allow_refresh = ?, access = ?, updated_at = ? WHERE id = ?",
		c.Name, jsonList(c.RedirectURIs), jsonList(c.PostLogoutRedirectURIs), boolInt(c.AllowRefresh), c.Access, now, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	c.UpdatedAt = fromUnix(now)
	if !c.AllowRefresh {
		_, err = s.db.ExecContext(ctx, "UPDATE refresh_tokens SET revoked_at = ? WHERE client_id = ? AND revoked_at IS NULL", now, c.ID)
	}
	return err
}

// RotateClientSecret replaces the secret of a web client and returns the new one.
func (s *Store) RotateClientSecret(ctx context.Context, id string) (string, error) {
	secret := NewToken()
	res, err := s.db.ExecContext(ctx, "UPDATE clients SET secret_hash = ?, updated_at = ? WHERE id = ? AND type = ?",
		HashToken(secret), s.unix(), id, ClientWeb)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrNotFound
	}
	return secret, nil
}

// DeleteClient removes a client; its auth requests and tokens cascade.
func (s *Store) DeleteClient(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM clients WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
