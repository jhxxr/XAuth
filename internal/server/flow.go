package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"xauth/internal/oidcprovider"
	"xauth/internal/store"
)

var passwordAMR = []string{"pwd"}
var webauthnAMR = []string{"webauthn"}

func callbackURL(id string) string { return oidcprovider.CallbackPath + "?id=" + url.QueryEscape(id) }

func loginPageURL(id string) string { return "/login?flow=" + url.QueryEscape(id) }

// canContinue reports whether the signed-in session may answer the request
// without asking for the password again.
func (s *Server) canContinue(info *sessionInfo, a *store.AuthRequest) bool {
	if info == nil {
		return false
	}
	if a.HintSubject != "" && a.HintSubject != info.user.ID {
		return false
	}
	if !a.AuthTime.IsZero() && (a.UserID != info.user.ID || !a.AuthTime.Equal(info.sess.AuthTime)) {
		return false
	}
	// prompt=login requires a fresh login. Once that login succeeded for this
	// pending request, the same browser may continue to the consent decision.
	if slices.Contains(a.Prompt, oidc.PromptLogin) && a.AuthTime.IsZero() {
		return false
	}
	if a.MaxAge != nil && s.st.Now().Sub(info.sess.AuthTime) > time.Duration(*a.MaxAge)*time.Second {
		return false
	}
	return true
}

// requiresConsent reports whether this request needs an explicit approval
// from the signed-in user. prompt=consent always forces the page; otherwise
// only scopes not previously approved for this client do.
func (s *Server) requiresConsent(ctx context.Context, a *store.AuthRequest, userID string) (bool, error) {
	if slices.Contains(a.Prompt, oidc.PromptConsent) {
		return true, nil
	}
	covered, err := s.st.ConsentCovers(ctx, userID, a.ClientID, a.Scopes)
	return !covered, err
}

func (s *Server) completeFlow(ctx context.Context, a *store.AuthRequest, userID string, authTime time.Time, amr []string) (string, bool, string) {
	c, allowed := s.appAccess(ctx, a, userID)
	if !allowed {
		s.denyApp(ctx, a, userID, c)
		return "", false, "access_denied"
	}
	needsConsent, err := s.requiresConsent(ctx, a, userID)
	if err != nil {
		return "", false, "internal"
	}
	if needsConsent {
		if err := s.st.AuthenticateAuthRequest(ctx, a.ID, userID, authTime, amr); err != nil {
			return "", false, "flow_expired"
		}
		return "", true, ""
	}
	if !s.approveAppWithAMR(ctx, a, userID, authTime, amr) {
		return "", false, "access_denied"
	}
	return callbackURL(a.ID), false, ""
}

// handleLoginStart is where /authorize sends the browser. A signed-in user
// who does not need to be asked anything goes straight back to the app.
func (s *Server) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	a, err := s.st.AuthRequestByID(r.Context(), id)
	if err != nil || a.Status != store.AuthPending {
		http.Redirect(w, r, "/login?error=flow_expired", http.StatusFound)
		return
	}
	info := sessionFrom(r.Context())
	ok := s.canContinue(info, a)

	if slices.Contains(a.Prompt, oidc.PromptNone) {
		if !ok {
			_ = s.st.DeleteAuthRequest(r.Context(), a.ID)
			op.AuthRequestError(w, r, oidcprovider.AuthRequestAdapter(a), oidc.ErrLoginRequired(), s.provider.OP)
			return
		}
		needsConsent, err := s.requiresConsent(r.Context(), a, info.user.ID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if needsConsent {
			_ = s.st.DeleteAuthRequest(r.Context(), a.ID)
			op.AuthRequestError(w, r, oidcprovider.AuthRequestAdapter(a), oidc.ErrInteractionRequired(), s.provider.OP)
			return
		}
		s.approve(w, r, a, info)
		return
	}
	asks := slices.ContainsFunc(a.Prompt, func(p string) bool {
		return p == oidc.PromptSelectAccount
	})
	if ok {
		needsConsent, err := s.requiresConsent(r.Context(), a, info.user.ID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if !needsConsent && !asks {
			s.approve(w, r, a, info)
			return
		}
		if err := s.st.AuthenticateAuthRequest(r.Context(), a.ID, info.user.ID, info.sess.AuthTime, a.AMR); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	http.Redirect(w, r, loginPageURL(a.ID), http.StatusFound)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request, a *store.AuthRequest, info *sessionInfo) {
	amr := a.AMR
	if len(amr) == 0 {
		amr = passwordAMR
	}
	if !s.approveAppWithAMR(r.Context(), a, info.user.ID, info.sess.AuthTime, amr) {
		http.Redirect(w, r, "/login?error=access_denied", http.StatusFound)
		return
	}
	http.Redirect(w, r, callbackURL(a.ID), http.StatusFound)
}

// handleCallback answers a cancelled request with access_denied and lets the
// library issue the code for an approved one.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if a, err := s.st.AuthRequestByID(r.Context(), r.URL.Query().Get("id")); err == nil && a.Status == store.AuthDenied {
		_ = s.st.DeleteAuthRequest(r.Context(), a.ID)
		op.AuthRequestError(w, r, oidcprovider.AuthRequestAdapter(a),
			oidc.ErrAccessDenied().WithDescription("the user cancelled the sign-in"), s.provider.OP)
		return
	}
	s.serveProvider(w, r)
}

type flowJSON struct {
	ID              string    `json:"id"`
	App             flowApp   `json:"app"`
	Scopes          []string  `json:"scopes"`
	RedirectTo      string    `json:"redirectTo"`
	LoginHint       string    `json:"loginHint,omitempty"`
	CanContinue     bool      `json:"canContinue"`
	RequiresConsent bool      `json:"requiresConsent"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type flowApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// redirectTarget shows where the user will be sent: the host for web
// redirects, the scheme for app-specific ones.
func redirectTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Host != "" {
		return u.Host
	}
	return u.Scheme + ":"
}

// pendingFlow loads a request that is still waiting for the user.
func (s *Server) pendingFlow(w http.ResponseWriter, r *http.Request) (*store.AuthRequest, bool) {
	a, err := s.st.AuthRequestByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil || a.Status != store.AuthPending {
		writeError(w, http.StatusGone, "flow_expired")
		return nil, false
	}
	return a, true
}

func (s *Server) handleFlow(w http.ResponseWriter, r *http.Request) {
	a, ok := s.pendingFlow(w, r)
	if !ok {
		return
	}
	c, err := s.st.ClientByID(r.Context(), a.ClientID)
	if err != nil {
		writeError(w, http.StatusGone, "flow_expired")
		return
	}
	needsConsent := slices.Contains(a.Prompt, oidc.PromptConsent)
	if info := sessionFrom(r.Context()); info != nil && s.canContinue(info, a) {
		var err error
		needsConsent, err = s.requiresConsent(r.Context(), a, info.user.ID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, flowJSON{
		ID: a.ID, App: flowApp{ID: c.ID, Name: c.Name}, Scopes: a.Scopes,
		RedirectTo: redirectTarget(a.RedirectURI), LoginHint: a.LoginHint,
		CanContinue: s.canContinue(sessionFrom(r.Context()), a), RequiresConsent: needsConsent,
		ExpiresAt: a.ExpiresAt,
	})
}

type redirectJSON struct {
	Redirect string `json:"redirect"`
}

func (s *Server) handleFlowContinue(w http.ResponseWriter, r *http.Request) {
	info := sessionFrom(r.Context())
	if info == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	a, ok := s.pendingFlow(w, r)
	if !ok {
		return
	}
	if !s.canContinue(info, a) {
		writeError(w, http.StatusConflict, "login_required")
		return
	}
	amr := a.AMR
	if len(amr) == 0 {
		amr = passwordAMR
	}
	if !s.approveAppWithAMR(r.Context(), a, info.user.ID, info.sess.AuthTime, amr) {
		writeError(w, http.StatusForbidden, "access_denied")
		return
	}
	writeJSON(w, http.StatusOK, redirectJSON{Redirect: callbackURL(a.ID)})
}

func (s *Server) handleFlowDeny(w http.ResponseWriter, r *http.Request) {
	a, ok := s.pendingFlow(w, r)
	if !ok {
		return
	}
	if err := s.st.DenyAuthRequest(r.Context(), a.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusGone, "flow_expired")
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, redirectJSON{Redirect: callbackURL(a.ID)})
}
