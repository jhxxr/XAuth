package server

import (
	"errors"
	"net/http"
	"time"

	"xauth/internal/password"
	"xauth/internal/store"
)

type userJSON struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	EmailVerified bool       `json:"emailVerified"`
	IsAdmin       bool       `json:"isAdmin"`
	Disabled      bool       `json:"disabled"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastLoginAt   *time.Time `json:"lastLoginAt"`
}

func toUserJSON(u *store.User) *userJSON {
	out := &userJSON{
		ID: u.ID, Email: u.Email, Name: u.Name, EmailVerified: u.EmailVerified,
		IsAdmin: u.IsAdmin, Disabled: u.Disabled, CreatedAt: u.CreatedAt,
	}
	if !u.LastLoginAt.IsZero() {
		t := u.LastLoginAt
		out.LastLoginAt = &t
	}
	return out
}

type sessionJSON struct {
	User          *userJSON `json:"user"`
	SignupEnabled bool      `json:"signupEnabled"`
	Issuer        string    `json:"issuer"`
	MailEnabled   bool      `json:"mailEnabled"`
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	resp := sessionJSON{SignupEnabled: s.cfg.SignupEnabled, Issuer: s.cfg.Issuer, MailEnabled: s.cfg.SMTP.Enabled()}
	if info := sessionFrom(r.Context()); info != nil {
		resp.User = toUserJSON(info.user)
	}
	writeJSON(w, http.StatusOK, resp)
}

type authResponse struct {
	User      *userJSON `json:"user"`
	Redirect  string    `json:"redirect,omitempty"`
	FlowError string    `json:"flowError,omitempty"`
}

// startSession replaces any current session with a new one for userID.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) (*store.Session, error) {
	if info := sessionFrom(r.Context()); info != nil {
		_ = s.st.DeleteSession(r.Context(), info.sess.TokenHash)
	}
	token, sess, err := s.st.CreateSession(r.Context(), userID, s.cfg.SessionTTL, store.SessionMeta{
		UserAgent: r.UserAgent(), IP: s.clientIP(r),
	})
	if err != nil {
		return nil, err
	}
	s.setCookie(w, token)
	return sess, nil
}

// finishSignIn approves the pending authorization request, if any, and
// writes the response.
func (s *Server) finishSignIn(w http.ResponseWriter, r *http.Request, status int, u *store.User, sess *store.Session, flow string) {
	resp := authResponse{User: toUserJSON(u)}
	if flow != "" {
		if a, err := s.st.AuthRequestByID(r.Context(), flow); err != nil || a.Status != store.AuthPending {
			resp.FlowError = "flow_expired"
		} else if !s.approveApp(r.Context(), a, u.ID, sess.AuthTime) {
			resp.FlowError = "access_denied"
		} else {
			resp.Redirect = callbackURL(flow)
		}
	}
	writeJSON(w, status, resp)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Flow     string `json:"flow"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	email := store.NormalizeEmail(req.Email)
	ip := s.clientIP(r)
	if !s.loginIP.allowed(ip) || !s.loginEmail.allowed(email) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	u, err := s.st.UserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}
	hash := ""
	if u != nil {
		hash = u.PasswordHash
	}
	if !password.Check(hash, req.Password) {
		s.loginIP.hit(ip)
		s.loginEmail.hit(email)
		s.audit(r.Context(), store.Event{Type: store.EvLoginFailure, ActorID: idOf(u), IP: ip,
			Detail: map[string]any{"email": email, "reason": "invalid_credentials"}})
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if u.Disabled {
		s.audit(r.Context(), store.Event{Type: store.EvLoginFailure, ActorID: u.ID, IP: ip,
			Detail: map[string]any{"reason": "account_disabled"}})
		writeError(w, http.StatusForbidden, "account_disabled")
		return
	}
	s.loginEmail.clear(email)
	_ = s.st.TouchLogin(r.Context(), u.ID)
	u.LastLoginAt = s.st.Now()
	sess, err := s.startSession(w, r, u.ID)
	if err == nil {
		s.audit(r.Context(), store.Event{Type: store.EvLoginSuccess, ActorID: u.ID, IP: ip})
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.finishSignIn(w, r, http.StatusOK, u, sess, req.Flow)
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Flow     string `json:"flow"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.SignupEnabled {
		writeError(w, http.StatusForbidden, "signup_disabled")
		return
	}
	var req registerRequest
	if !decode(w, r, &req) {
		return
	}
	ip := s.clientIP(r)
	if !s.signupIP.allowed(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	email, ok := validEmail(req.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_email")
		return
	}
	name, ok := cleanName(req.Name, 0, 64)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_name")
		return
	}
	if err := password.Validate(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, passwordError(err))
		return
	}
	s.signupIP.hit(ip)
	hash, err := password.Hash(req.Password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	u := &store.User{Email: email, Name: name, PasswordHash: hash}
	if err := s.st.CreateUser(r.Context(), u); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "email_taken")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvRegister, ActorID: u.ID, IP: s.clientIP(r)})
	sess, err := s.startSession(w, r, u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.finishSignIn(w, r, http.StatusCreated, u, sess, req.Flow)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if info := sessionFrom(r.Context()); info != nil {
		if err := s.st.DeleteSession(r.Context(), info.sess.TokenHash); err != nil {
			s.internalError(w, r, err)
			return
		}
		s.audit(r.Context(), store.Event{Type: store.EvLogout, ActorID: info.user.ID, IP: s.clientIP(r)})
	}
	s.clearCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (*sessionInfo, bool) {
	info := sessionFrom(r.Context())
	if info == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return nil, false
	}
	return info, true
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name, 0, 64)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_name")
		return
	}
	u := *info.user
	u.Name = name
	if err := s.st.UpdateUser(r.Context(), &u); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserJSON(&u)})
}

// handleChangePassword signs out every other session and revokes the
// user's tokens, so a leaked password stops working everywhere.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !password.Check(info.user.PasswordHash, req.Current) {
		writeError(w, http.StatusBadRequest, "wrong_password")
		return
	}
	if err := password.Validate(req.New); err != nil {
		writeError(w, http.StatusBadRequest, passwordError(err))
		return
	}
	hash, err := password.Hash(req.New)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	u := *info.user
	u.PasswordHash = hash
	if err := s.st.UpdateUser(r.Context(), &u); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvPasswordChange, ActorID: u.ID, IP: s.clientIP(r)})
	if err := s.st.RevokeUserAccess(r.Context(), u.ID, info.sess.TokenHash); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
