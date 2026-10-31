// Package oidcprovider wires the zitadel/oidc OpenID Provider to XAuth's store.
package oidcprovider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/rs/cors"
	"github.com/zitadel/oidc/v3/pkg/op"

	"xauth/internal/config"
	"xauth/internal/store"
)

// Paths served by XAuth itself rather than the library.
const (
	// LoginStartPath receives the browser from /authorize.
	LoginStartPath = "/login/start"
	// CallbackPath is where the browser returns once the request is decided.
	CallbackPath = "/authorize/callback"
	// LogoutPath is the sign-out confirmation page.
	LogoutPath = "/logout"
)

// Provider is the OpenID Provider and its storage.
type Provider struct {
	OP      *op.Provider
	Storage *Storage
}

// New loads (or creates) signing keys and builds the provider.
func New(ctx context.Context, cfg config.Config, st *store.Store) (*Provider, error) {
	keys, err := loadKeys(ctx, st)
	if err != nil {
		return nil, fmt.Errorf("load signing keys: %w", err)
	}
	cryptoKey, err := st.Setting(ctx, "oidc_crypto_key", 32)
	if err != nil {
		return nil, fmt.Errorf("load crypto key: %w", err)
	}
	storage := &Storage{st: st, cfg: cfg, keys: keys}

	opCfg := &op.Config{
		CryptoKeyId:              "1",
		DefaultLogoutRedirectURI: LogoutPath + "?done=1",
		CodeMethodS256:           true,
		AuthMethodPost:           true,
		GrantTypeRefreshToken:    true,
		SupportedScopes:          []string{"openid", "profile", "email", "offline_access"},
		SupportedClaims: []string{
			"sub", "aud", "exp", "iat", "iss", "auth_time", "nonce", "amr", "azp", "at_hash", "c_hash",
			"name", "preferred_username", "updated_at", "email", "email_verified",
		},
	}
	copy(opCfg.CryptoKey[:], cryptoKey)

	opts := []op.Option{
		// Token, userinfo and discovery are called cross-origin by SPAs.
		// Cookies are never needed there, so credentials stay off.
		op.WithCORSOptions(&cors.Options{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{http.MethodGet, http.MethodHead, http.MethodPost},
			AllowedHeaders: []string{"Authorization", "Content-Type", "Accept"},
		}),
	}
	if cfg.Dev {
		opts = append(opts, op.WithAllowInsecure())
	}
	provider, err := op.NewOpenIDProvider(cfg.Issuer, opCfg, storage, opts...)
	if err != nil {
		return nil, err
	}
	return &Provider{OP: provider, Storage: storage}, nil
}

// BrowserSession identifies the signed-in browser to the logout handler.
type BrowserSession struct {
	TokenHash string
	UserID    string
}

type browserSessionKey struct{}

// WithBrowserSession attaches the browser's session to a request context.
func WithBrowserSession(ctx context.Context, bs BrowserSession) context.Context {
	return context.WithValue(ctx, browserSessionKey{}, bs)
}

func browserSessionFrom(ctx context.Context) (BrowserSession, bool) {
	bs, ok := ctx.Value(browserSessionKey{}).(BrowserSession)
	return bs, ok
}
