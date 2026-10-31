package server

import (
	"net/http"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// serveDiscovery publishes the library's discovery document, trimmed to what
// XAuth accepts: the authorization code flow with PKCE S256 and refresh
// tokens. The library always lists implicit, JWT bearer and device grants.
func (s *Server) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	o := s.provider.OP
	op.NewIssuerInterceptor(o.IssuerFromRequest).HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := op.CreateDiscoveryConfig(r.Context(), o, s.provider.Storage)
		cfg.ResponseTypesSupported = []string{string(oidc.ResponseTypeCode)}
		cfg.GrantTypesSupported = []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}
		cfg.ResponseModesSupported = []string{string(oidc.ResponseModeQuery), string(oidc.ResponseModeFragment), string(oidc.ResponseModeFormPost)}
		cfg.CodeChallengeMethodsSupported = []oidc.CodeChallengeMethod{oidc.CodeChallengeMethodS256}
		cfg.DeviceAuthorizationEndpoint = ""
		cfg.RequestParameterSupported = false
		w.Header().Set("Access-Control-Allow-Origin", "*")
		op.Discover(w, cfg)
	})(w, r)
}
