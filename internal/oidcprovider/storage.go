package oidcprovider

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"xauth/internal/config"
	"xauth/internal/store"
)

// Storage implements op.Storage on top of the SQLite store.
type Storage struct {
	st   *store.Store
	cfg  config.Config
	keys []*signingKey
}

var (
	_ op.Storage                        = (*Storage)(nil)
	_ op.CanSetUserinfoFromRequest      = (*Storage)(nil)
	_ op.CanTerminateSessionFromRequest = (*Storage)(nil)
)

// pkceChallenge matches an S256 challenge: base64url of a SHA-256 digest.
var pkceChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

const maxParamLen = 2048

// CreateAuthRequest stores an authorization request after the library has
// validated the client, redirect URI and scopes. XAuth additionally requires
// the code flow with PKCE S256 for every client.
func (s *Storage) CreateAuthRequest(ctx context.Context, req *oidc.AuthRequest, hintSubject string) (op.AuthRequest, error) {
	if req.ResponseType != oidc.ResponseTypeCode {
		return nil, oidc.ErrInvalidRequest().WithDescription("only response_type=code is supported")
	}
	if req.CodeChallenge == "" {
		return nil, oidc.ErrInvalidRequest().WithDescription("code_challenge is required (PKCE)")
	}
	if req.CodeChallengeMethod != oidc.CodeChallengeMethodS256 {
		return nil, oidc.ErrInvalidRequest().WithDescription("code_challenge_method must be S256")
	}
	if !pkceChallenge.MatchString(req.CodeChallenge) {
		return nil, oidc.ErrInvalidRequest().WithDescription("code_challenge is malformed")
	}
	if len(req.State) > maxParamLen || len(req.Nonce) > maxParamLen || len(req.LoginHint) > 320 {
		return nil, oidc.ErrInvalidRequest().WithDescription("state, nonce or login_hint is too long")
	}
	c, err := s.st.ClientByID(ctx, req.ClientID)
	if err != nil {
		return nil, oidc.ErrInvalidClient().WithParent(err)
	}
	scopes := []string(req.Scopes)
	if !c.AllowRefresh {
		scopes = slices.DeleteFunc(slices.Clone(scopes), func(v string) bool { return v == oidc.ScopeOfflineAccess })
	}
	a := &store.AuthRequest{
		ClientID:        req.ClientID,
		RedirectURI:     req.RedirectURI,
		Scopes:          scopes,
		State:           req.State,
		Nonce:           req.Nonce,
		ResponseType:    string(req.ResponseType),
		ResponseMode:    string(req.ResponseMode),
		CodeChallenge:   req.CodeChallenge,
		ChallengeMethod: string(req.CodeChallengeMethod),
		Prompt:          []string(req.Prompt),
		MaxAge:          req.MaxAge,
		LoginHint:       req.LoginHint,
		HintSubject:     hintSubject,
	}
	if err := s.st.CreateAuthRequest(ctx, a, s.cfg.AuthRequestTTL); err != nil {
		return nil, err
	}
	return &authRequest{a}, nil
}

func (s *Storage) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	a, err := s.st.AuthRequestByID(ctx, id)
	if err != nil {
		return nil, oidc.ErrInvalidRequest().WithDescription("authorization request not found or expired").WithParent(err)
	}
	return &authRequest{a}, nil
}

// AuthRequestByCode redeems a code exactly once. Codes are kept after use
// for a while so a second use revokes everything the first use produced.
func (s *Storage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	a, err := s.st.RedeemAuthCode(ctx, code, s.cfg.RefreshTokenTTL)
	if err != nil {
		return nil, err
	}
	return &authRequest{a}, nil
}

func (s *Storage) SaveAuthCode(ctx context.Context, id, code string) error {
	return s.st.SaveAuthCode(ctx, id, code, s.cfg.AuthCodeTTL)
}

func (s *Storage) DeleteAuthRequest(ctx context.Context, id string) error {
	return s.st.DeleteAuthRequest(ctx, id)
}

// grant builds the token records for a token request.
func (s *Storage) grant(ctx context.Context, req op.TokenRequest) (*store.AccessToken, *store.RefreshToken, string, error) {
	var family, clientID, rotateFrom string
	var amr []string
	var authTime time.Time
	var refreshScopes []string
	switch r := req.(type) {
	case *authRequest:
		family, clientID, amr, authTime = r.a.ID, r.a.ClientID, r.a.AMR, r.a.AuthTime
		refreshScopes = r.a.Scopes
	case *refreshRequest:
		family, clientID, amr, authTime = r.t.FamilyID, r.t.ClientID, r.t.AMR, r.t.AuthTime
		refreshScopes, rotateFrom = r.t.Scopes, r.t.ID
	default:
		return nil, nil, "", oidc.ErrUnsupportedGrantType()
	}
	if _, err := s.st.ActiveUser(ctx, req.GetSubject()); err != nil {
		return nil, nil, "", oidc.ErrInvalidGrant().WithDescription("user is disabled or deleted")
	}
	access := &store.AccessToken{
		FamilyID: family, ClientID: clientID, UserID: req.GetSubject(),
		Scopes: req.GetScopes(), Audience: req.GetAudience(),
	}
	refresh := &store.RefreshToken{
		FamilyID: family, ClientID: clientID, UserID: req.GetSubject(),
		Scopes: refreshScopes, Audience: req.GetAudience(), AMR: amr, AuthTime: authTime,
	}
	return access, refresh, rotateFrom, nil
}

func (s *Storage) CreateAccessToken(ctx context.Context, req op.TokenRequest) (string, time.Time, error) {
	access, _, _, err := s.grant(ctx, req)
	if err != nil {
		return "", time.Time{}, err
	}
	if _, err := s.st.IssueTokens(ctx, access, s.cfg.AccessTokenTTL, nil, 0, ""); err != nil {
		return "", time.Time{}, err
	}
	return access.ID, access.ExpiresAt, nil
}

// CreateAccessAndRefreshTokens issues tokens and rotates the refresh token.
func (s *Storage) CreateAccessAndRefreshTokens(ctx context.Context, req op.TokenRequest, _ string) (string, string, time.Time, error) {
	access, refresh, rotateFrom, err := s.grant(ctx, req)
	if err != nil {
		return "", "", time.Time{}, err
	}
	raw, err := s.st.IssueTokens(ctx, access, s.cfg.AccessTokenTTL, refresh, s.cfg.RefreshTokenTTL, rotateFrom)
	if errors.Is(err, store.ErrTokenReused) {
		_ = s.st.RevokeFamily(ctx, access.FamilyID)
		return "", "", time.Time{}, oidc.ErrInvalidGrant().WithDescription("refresh token already used")
	}
	if err != nil {
		return "", "", time.Time{}, err
	}
	return access.ID, raw, access.ExpiresAt, nil
}

// TokenRequestByRefreshToken returns the grant behind a refresh token.
// Presenting a revoked token revokes its whole family: a rotated token is
// only ever presented again if it was copied.
func (s *Storage) TokenRequestByRefreshToken(ctx context.Context, raw string) (op.RefreshTokenRequest, error) {
	t, err := s.st.RefreshTokenByRaw(ctx, raw)
	if err != nil {
		return nil, op.ErrInvalidRefreshToken
	}
	if t.Revoked {
		if err := s.st.RevokeFamily(ctx, t.FamilyID); err != nil {
			return nil, err
		}
		_ = s.st.Record(ctx, store.Event{Type: store.EvTokenReuse, ActorID: t.UserID, SubjectType: "app", SubjectID: t.ClientID,
			Detail: map[string]any{"kind": "refresh"}})
		return nil, store.ErrTokenReused
	}
	if !t.Active(s.st.Now()) {
		return nil, op.ErrInvalidRefreshToken
	}
	if _, err := s.st.ActiveUser(ctx, t.UserID); err != nil {
		return nil, op.ErrInvalidRefreshToken
	}
	return &refreshRequest{t: t, scopes: t.Scopes}, nil
}

// TerminateSession is required by op.AuthStorage but unused, because
// TerminateSessionFromRequest takes precedence.
func (s *Storage) TerminateSession(context.Context, string, string) error { return nil }

// TerminateSessionFromRequest ends the browser session for RP-initiated
// logout. The session is only ended when a valid id_token_hint names the
// signed-in user; otherwise the user confirms on the XAuth sign-out page.
func (s *Storage) TerminateSessionFromRequest(ctx context.Context, req *op.EndSessionRequest) (string, error) {
	bs, ok := browserSessionFrom(ctx)
	if req.UserID == "" {
		return LogoutPath, nil
	}
	if ok && bs.UserID == req.UserID {
		if err := s.st.DeleteSession(ctx, bs.TokenHash); err != nil {
			return "", err
		}
	}
	return req.RedirectURI, nil
}

// RevokeToken implements RFC 7009. Unknown tokens are not an error.
// Revoking a refresh token revokes every token of its grant.
func (s *Storage) RevokeToken(ctx context.Context, tokenOrID, _ string, clientID string) *oidc.Error {
	t, err := s.st.RefreshTokenByID(ctx, tokenOrID)
	if errors.Is(err, store.ErrNotFound) {
		t, err = s.st.RefreshTokenByRaw(ctx, tokenOrID)
	}
	if err == nil {
		if t.ClientID != clientID {
			return oidc.ErrInvalidClient().WithDescription("token was issued to another client")
		}
		if err := s.st.RevokeFamily(ctx, t.FamilyID); err != nil {
			return oidc.ErrServerError().WithParent(err)
		}
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return oidc.ErrServerError().WithParent(err)
	}
	at, err := s.st.AccessTokenByID(ctx, tokenOrID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return oidc.ErrServerError().WithParent(err)
	}
	if at.ClientID != clientID {
		return oidc.ErrInvalidClient().WithDescription("token was issued to another client")
	}
	if err := s.st.RevokeAccessToken(ctx, at.ID); err != nil {
		return oidc.ErrServerError().WithParent(err)
	}
	return nil
}

func (s *Storage) GetRefreshTokenInfo(ctx context.Context, clientID, raw string) (string, string, error) {
	t, err := s.st.RefreshTokenByRaw(ctx, raw)
	if err != nil {
		return "", "", op.ErrInvalidRefreshToken
	}
	return t.UserID, t.ID, nil
}

// currentKeys reads the live signing keys so rotation takes effect without a restart.
func (s *Storage) currentKeys(ctx context.Context) ([]*signingKey, error) {
	keys, err := loadKeys(ctx, s.st)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errors.New("no signing key")
	}
	return keys, nil
}

func (s *Storage) SigningKey(ctx context.Context) (op.SigningKey, error) {
	keys, err := s.currentKeys(ctx)
	if err != nil {
		return nil, err
	}
	return keys[0], nil
}

func (s *Storage) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}

func (s *Storage) KeySet(ctx context.Context) ([]op.Key, error) {
	keys, err := s.currentKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]op.Key, len(keys))
	for i, k := range keys {
		out[i] = &publicKey{id: k.id, key: &k.key.PublicKey}
	}
	return out, nil
}

func (s *Storage) GetClientByClientID(ctx context.Context, id string) (op.Client, error) {
	c, err := s.st.ClientByID(ctx, id)
	if err != nil {
		return nil, oidc.ErrInvalidClient().WithDescription("unknown client").WithParent(err)
	}
	return &client{c: c, devMode: s.cfg.Dev, idTokenTT: s.cfg.IDTokenTTL}, nil
}

func (s *Storage) AuthorizeClientIDSecret(ctx context.Context, id, secret string) error {
	c, err := s.st.ClientByID(ctx, id)
	if err != nil || !c.Confidential() || c.SecretHash == "" || secret == "" || !store.EqualHash(secret, c.SecretHash) {
		return oidc.ErrInvalidClient().WithDescription("invalid client credentials")
	}
	return nil
}

// SetUserinfoFromScopes is deprecated in the library; SetUserinfoFromRequest does the work.
func (s *Storage) SetUserinfoFromScopes(context.Context, *oidc.UserInfo, string, string, []string) error {
	return nil
}

func (s *Storage) SetUserinfoFromRequest(ctx context.Context, info *oidc.UserInfo, req op.IDTokenRequest, scopes []string) error {
	u, err := s.st.ActiveUser(ctx, req.GetSubject())
	if err != nil {
		return oidc.ErrInvalidGrant().WithDescription("user is disabled or deleted")
	}
	fillUserinfo(info, u, scopes)
	return nil
}

// activeAccessToken returns the stored record of an unrevoked, unexpired
// access token for subject, and its active user.
func (s *Storage) activeAccessToken(ctx context.Context, tokenID, subject string) (*store.AccessToken, *store.User, error) {
	at, err := s.st.AccessTokenByID(ctx, tokenID)
	if err != nil || !at.Active(s.st.Now()) || at.UserID != subject {
		return nil, nil, oidc.ErrAccessDenied().WithDescription("access token is invalid, expired or revoked")
	}
	u, err := s.st.ActiveUser(ctx, at.UserID)
	if err != nil {
		return nil, nil, oidc.ErrAccessDenied().WithDescription("user is disabled or deleted")
	}
	return at, u, nil
}

func (s *Storage) SetUserinfoFromToken(ctx context.Context, info *oidc.UserInfo, tokenID, subject, _ string) error {
	at, u, err := s.activeAccessToken(ctx, tokenID, subject)
	if err != nil {
		return err
	}
	fillUserinfo(info, u, at.Scopes)
	return nil
}

// SetIntrospectionFromToken answers only for tokens issued to the calling client.
func (s *Storage) SetIntrospectionFromToken(ctx context.Context, resp *oidc.IntrospectionResponse, tokenID, subject, clientID string) error {
	at, u, err := s.activeAccessToken(ctx, tokenID, subject)
	if err != nil {
		return err
	}
	if at.ClientID != clientID && !slices.Contains(at.Audience, clientID) {
		return oidc.ErrAccessDenied().WithDescription("token was issued to another client")
	}
	info := new(oidc.UserInfo)
	fillUserinfo(info, u, at.Scopes)
	resp.SetUserInfo(info)
	resp.Scope = at.Scopes
	resp.ClientID = at.ClientID
	resp.TokenType = oidc.BearerToken
	resp.Expiration = oidc.FromTime(at.ExpiresAt)
	resp.IssuedAt = oidc.FromTime(at.CreatedAt)
	resp.Audience = at.Audience
	resp.JWTID = at.ID
	return nil
}

func (s *Storage) GetPrivateClaimsFromScopes(context.Context, string, string, []string) (map[string]any, error) {
	return nil, nil
}

// JWT profile grants are not supported: no client has registered keys.
func (s *Storage) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, errors.New("jwt profile grant is not supported")
}

func (s *Storage) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, errors.New("jwt profile grant is not supported")
}

func (s *Storage) Health(ctx context.Context) error { return s.st.Ping(ctx) }

// fillUserinfo sets the claims each standard scope grants.
func fillUserinfo(info *oidc.UserInfo, u *store.User, scopes []string) {
	info.Subject = u.ID
	if hasScope(scopes, oidc.ScopeProfile) {
		info.Name = u.Name
		info.PreferredUsername = u.Email
		info.UpdatedAt = oidc.FromTime(u.UpdatedAt)
	}
	if hasScope(scopes, oidc.ScopeEmail) {
		info.Email = u.Email
		info.EmailVerified = oidc.Bool(u.EmailVerified)
	}
}
