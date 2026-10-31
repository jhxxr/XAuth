// Package server is the XAuth HTTP layer: the OIDC endpoints, the JSON API
// used by the web UI, and the embedded single-page app.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"xauth/internal/config"
	"xauth/internal/mailer"
	"xauth/internal/oidcprovider"
	"xauth/internal/store"
	"xauth/internal/xmail"
)

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	cfg        config.Config
	st         *store.Store
	provider   *oidcprovider.Provider
	ui         fs.FS
	log        *slog.Logger
	loginIP    *limiter
	loginEmail *limiter
	signupIP   *limiter
	// mail is nil when xmail is not configured.
	mail    *xmail.Client
	mailer  mailer.Sender
	resetIP *limiter
	views   viewLog
}

// New builds a server. ui is the built web app (may be empty in tests).
func New(cfg config.Config, st *store.Store, p *oidcprovider.Provider, ui fs.FS, log *slog.Logger) *Server {
	s := &Server{
		cfg:        cfg,
		st:         st,
		provider:   p,
		ui:         ui,
		log:        log,
		loginIP:    newLimiter(30, 15*time.Minute),
		loginEmail: newLimiter(10, 15*time.Minute),
		signupIP:   newLimiter(10, time.Hour),
		resetIP:    newLimiter(5, time.Hour),
	}
	if cfg.MailEnabled() {
		c, err := xmail.New(cfg.XMailBaseURL, cfg.XMailAPIKey.Reveal())
		if err != nil {
			log.Error("xmail disabled", "err", err)
		} else {
			s.mail = c
		}
	}
	s.mailer = mailer.New(cfg.SMTP)
	return s
}

// SetMailer replaces the outbound sender. Tests use it; production always
// uses the SMTP sender built in New.
func (s *Server) SetMailer(m mailer.Sender) { s.mailer = m }

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.accessLog)
	r.Use(securityHeaders)

	r.Group(func(r chi.Router) {
		r.Use(s.loadSession)

		r.Route("/api", func(r chi.Router) {
			r.Use(noStore)
			r.Use(s.sameOrigin)
			r.Get("/session", s.handleSession)
			r.Post("/login", s.handleLogin)
			r.Post("/register", s.handleRegister)
			r.Post("/verify", s.handleVerify)
			r.Post("/forgot-password", s.handleForgotPassword)
			r.Post("/reset-password", s.handleResetPassword)
			r.Post("/logout", s.handleLogout)
			r.Patch("/account", s.handleUpdateAccount)
			r.Put("/account/avatar", s.handleSetAvatar)
			r.Delete("/account/avatar", s.handleDeleteAvatar)
			r.Get("/users/{id}/avatar", s.handleAvatar)
			r.Post("/account/password", s.handleChangePassword)
			r.Post("/account/verify-email", s.handleRequestVerify)
			r.Get("/account/passkeys", s.handleListPasskeys)
			r.Delete("/account/passkeys/{id}", s.handleDeletePasskey)
			r.Post("/account/passkeys/register/begin", s.handlePasskeyRegisterBegin)
			r.Post("/account/passkeys/register/finish/{id}", s.handlePasskeyRegisterFinish)
			r.Post("/auth/passkey/begin", s.handlePasskeyLoginBegin)
			r.Post("/auth/passkey/finish/{id}", s.handlePasskeyLoginFinish)
			r.Get("/sessions", s.handleListSessions)
			r.Delete("/sessions/{id}", s.handleDeleteSession)
			r.Post("/sessions/revoke-others", s.handleRevokeOtherSessions)
			r.Get("/grants", s.handleListGrants)
			r.Delete("/grants/{clientID}", s.handleRevokeGrant)
			r.Get("/totp", s.handleListMyTotp)
			r.Post("/totp", s.handleCreateMyTotp)
			r.Patch("/totp/{id}", s.handleUpdateMyTotp)
			r.Delete("/totp/{id}", s.handleDeleteMyTotp)
			r.Get("/flow/{id}", s.handleFlow)
			r.Post("/flow/{id}/continue", s.handleFlowContinue)
			r.Post("/flow/{id}/deny", s.handleFlowDeny)
			r.Route("/mail", func(r chi.Router) {
				r.Get("/status", s.handleMailStatus)
				r.Get("/mailboxes", s.handleMyMailboxes)
				r.Get("/mailboxes/{address}", s.handleMyMailbox)
				r.Get("/mailboxes/{address}/stats", s.handleMyMailboxStats)
				r.Get("/mailboxes/{address}/services", s.handleMyMailboxServices)
				r.Get("/mailboxes/{address}/emails", s.handleMyEmails)
				r.Get("/mailboxes/{address}/verification-code", s.handleMyCode)
				r.Get("/emails/{id}", s.handleMyEmail)
				r.Get("/emails/{id}/html", s.handleMyEmailHTML)
				r.Post("/emails/{id}/read", s.handleMyEmailRead)
				r.Post("/emails/{id}/star", s.handleMyEmailStar)
			})
			r.Route("/admin", func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Patch("/users/{id}", s.handleUpdateUser)
				r.Post("/users/{id}/signout", s.handleForceSignOut)
				r.Get("/events", s.handleListEvents)
				r.Get("/keys", s.handleListKeys)
				r.Post("/keys", s.handleRotateKey)
				r.Post("/keys/{id}/retire", s.handleRetireKey)
				r.Get("/apps", s.handleListApps)
				r.Post("/apps", s.handleCreateApp)
				r.Patch("/apps/{id}", s.handleUpdateApp)
				r.Post("/apps/{id}/secret", s.handleRotateSecret)
				r.Delete("/apps/{id}", s.handleDeleteApp)
				r.Get("/totp", s.handleListAdminTotp)
				r.Post("/totp", s.handleCreateAdminTotp)
				r.Patch("/totp/{id}", s.handleUpdateAdminTotp)
				r.Delete("/totp/{id}", s.handleDeleteAdminTotp)
				r.Put("/totp/{id}/grants", s.handleAssignTotp)
				r.Get("/mail-grants", s.handleListMailGrants)
				r.Put("/mail-grants/{address}", s.handleSetMailGrants)
				r.Delete("/mail-grants/{address}", s.handleDeleteMailGrants)
				r.Get("/mail-html/{id}", s.handleAdminMailHTML)
				r.HandleFunc("/mail/*", s.handleMailProxy)
			})
			r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeError(w, http.StatusNotFound, "not_found") })
			r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			})
		})

		r.Get(oidcprovider.LoginStartPath, s.handleLoginStart)
		r.HandleFunc(oidcprovider.CallbackPath, s.handleCallback)
		// end_session needs the browser session to sign it out.
		r.HandleFunc("/end_session", s.serveProvider)
	})

	r.Get("/.well-known/openid-configuration", s.serveDiscovery)
	for _, p := range []string{
		"/authorize", "/oauth/token", "/oauth/introspect",
		"/userinfo", "/revoke", "/keys", "/healthz", "/ready",
	} {
		r.HandleFunc(p, s.serveProvider)
	}

	r.Get("/*", s.serveSPA)
	return r
}

// serveProvider hands a request to the library's own chi router.
func (s *Server) serveProvider(w http.ResponseWriter, r *http.Request) {
	ctx := store.WithClientIP(r.Context(), s.clientIP(r))
	if info := sessionFrom(ctx); info != nil {
		ctx = oidcprovider.WithBrowserSession(ctx, oidcprovider.BrowserSession{TokenHash: info.sess.TokenHash, UserID: info.user.ID})
	}
	// Clear the outer route context so the inner router routes from scratch.
	ctx = context.WithValue(ctx, chi.RouteCtxKey, (*chi.Context)(nil))
	s.provider.OP.HttpHandler().ServeHTTP(w, r.WithContext(ctx))
}

type sessionInfo struct {
	sess  *store.Session
	user  *store.User
	token string
}

type sessionKey struct{}

func sessionFrom(ctx context.Context) *sessionInfo {
	info, _ := ctx.Value(sessionKey{}).(*sessionInfo)
	return info
}

func (s *Server) cookieName() string {
	if s.cfg.SecureCookies() {
		return "__Host-xauth_session"
	}
	return "xauth_session"
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: token, Path: "/",
		MaxAge: int(s.cfg.SessionTTL.Seconds()), HttpOnly: true,
		Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

// loadSession attaches the signed-in user, if any, to the request.
func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.cookieName())
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		sess, err := s.st.SessionByToken(ctx, c.Value)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.clearCookie(w)
			} else {
				s.log.Error("load session", "err", err)
			}
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.st.ActiveUser(ctx, sess.UserID)
		if err != nil {
			_ = s.st.DeleteSession(ctx, sess.TokenHash)
			s.clearCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		if s.st.Now().Sub(sess.LastSeen) > time.Hour {
			if err := s.st.TouchSession(ctx, sess.TokenHash, s.cfg.SessionTTL); err == nil {
				s.setCookie(w, c.Value)
			}
		}
		ctx = context.WithValue(ctx, sessionKey{}, &sessionInfo{sess: sess, user: user, token: c.Value})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// sameOrigin blocks cross-site requests that change state. Browsers send
// Sec-Fetch-Site or Origin; requests with neither come from non-browser
// clients, which cannot carry a victim's cookies.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !s.originAllowed(r) {
			writeError(w, http.StatusForbidden, "forbidden_origin")
			return
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if origin == s.cfg.Issuer {
		return true
	}
	if s.cfg.Dev {
		if u, err := url.Parse(origin); err == nil && isLoopbackHost(u.Hostname()) {
			return true
		}
	}
	return false
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := sessionFrom(r.Context())
		if info == nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if !info.user.IsAdmin {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", status, "ms", time.Since(start).Milliseconds())
	})
}
