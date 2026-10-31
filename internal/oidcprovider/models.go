package oidcprovider

import (
	"log/slog"
	"net/url"
	"slices"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"xauth/internal/store"
)

// client adapts store.Client to op.Client.
type client struct {
	c         *store.Client
	devMode   bool
	idTokenTT time.Duration
}

var _ op.Client = (*client)(nil)

func (c *client) GetID() string                    { return c.c.ID }
func (c *client) RedirectURIs() []string           { return c.c.RedirectURIs }
func (c *client) PostLogoutRedirectURIs() []string { return c.c.PostLogoutRedirectURIs }

func (c *client) ApplicationType() op.ApplicationType {
	switch c.c.Type {
	case store.ClientWeb:
		return op.ApplicationTypeWeb
	case store.ClientSPA:
		return op.ApplicationTypeUserAgent
	default:
		return op.ApplicationTypeNative
	}
}

func (c *client) AuthMethod() oidc.AuthMethod {
	if c.c.Confidential() {
		return oidc.AuthMethodBasic
	}
	return oidc.AuthMethodNone
}

func (c *client) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}

func (c *client) GrantTypes() []oidc.GrantType {
	g := []oidc.GrantType{oidc.GrantTypeCode}
	if c.c.AllowRefresh {
		g = append(g, oidc.GrantTypeRefreshToken)
	}
	return g
}

func (c *client) LoginURL(id string) string           { return LoginStartPath + "?id=" + url.QueryEscape(id) }
func (c *client) AccessTokenType() op.AccessTokenType { return op.AccessTokenTypeJWT }
func (c *client) IDTokenLifetime() time.Duration      { return c.idTokenTT }

// DevMode lets SPA clients use http:// redirect URIs while XAUTH_DEV is on.
func (c *client) DevMode() bool { return c.devMode }

func (c *client) RestrictAdditionalIdTokenScopes() func([]string) []string {
	return func(s []string) []string { return s }
}

func (c *client) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return func(s []string) []string { return s }
}

// IsScopeAllowed rejects custom scopes; only the standard OIDC scopes pass.
func (c *client) IsScopeAllowed(string) bool { return false }

// IDTokenUserinfoClaimsAssertion puts profile and email claims in the ID
// token as well as at the userinfo endpoint, which many apps expect.
func (c *client) IDTokenUserinfoClaimsAssertion() bool { return true }
func (c *client) ClockSkew() time.Duration             { return 0 }

// authRequest adapts store.AuthRequest to op.AuthRequest.
type authRequest struct{ a *store.AuthRequest }

var _ op.AuthRequest = (*authRequest)(nil)

// AuthRequestAdapter wraps a stored request so the HTTP layer can pass it to
// op.AuthRequestError.
func AuthRequestAdapter(a *store.AuthRequest) op.AuthRequest { return &authRequest{a} }

func (r *authRequest) GetID() string                      { return r.a.ID }
func (r *authRequest) GetACR() string                     { return "" }
func (r *authRequest) GetAMR() []string                   { return r.a.AMR }
func (r *authRequest) GetAudience() []string              { return []string{r.a.ClientID} }
func (r *authRequest) GetAuthTime() time.Time             { return r.a.AuthTime }
func (r *authRequest) GetClientID() string                { return r.a.ClientID }
func (r *authRequest) GetNonce() string                   { return r.a.Nonce }
func (r *authRequest) GetRedirectURI() string             { return r.a.RedirectURI }
func (r *authRequest) GetScopes() []string                { return r.a.Scopes }
func (r *authRequest) GetState() string                   { return r.a.State }
func (r *authRequest) GetSubject() string                 { return r.a.UserID }
func (r *authRequest) Done() bool                         { return r.a.Status == store.AuthApproved }
func (r *authRequest) GetResponseType() oidc.ResponseType { return oidc.ResponseType(r.a.ResponseType) }
func (r *authRequest) GetResponseMode() oidc.ResponseMode { return oidc.ResponseMode(r.a.ResponseMode) }

func (r *authRequest) GetCodeChallenge() *oidc.CodeChallenge {
	return &oidc.CodeChallenge{Challenge: r.a.CodeChallenge, Method: oidc.CodeChallengeMethod(r.a.ChallengeMethod)}
}

func (r *authRequest) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", r.a.ID), slog.String("client_id", r.a.ClientID))
}

// refreshRequest adapts store.RefreshToken to op.RefreshTokenRequest.
type refreshRequest struct {
	t      *store.RefreshToken
	scopes []string
}

var _ op.RefreshTokenRequest = (*refreshRequest)(nil)

func (r *refreshRequest) GetAMR() []string            { return r.t.AMR }
func (r *refreshRequest) GetAudience() []string       { return r.t.Audience }
func (r *refreshRequest) GetAuthTime() time.Time      { return r.t.AuthTime }
func (r *refreshRequest) GetClientID() string         { return r.t.ClientID }
func (r *refreshRequest) GetScopes() []string         { return r.scopes }
func (r *refreshRequest) GetSubject() string          { return r.t.UserID }
func (r *refreshRequest) SetCurrentScopes(s []string) { r.scopes = s }

// hasScope reports whether scopes contains s.
func hasScope(scopes []string, s string) bool { return slices.Contains(scopes, s) }
