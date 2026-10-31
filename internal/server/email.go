package server

import (
	"errors"
	"net/http"
	"strings"

	"xauth/internal/mailer"
	"xauth/internal/password"
	"xauth/internal/store"
)

// sendMail delivers a message. Tests replace s.mailer to capture it.
func (s *Server) sendMail(to, subject, text string) error {
	if s.mailer == nil {
		return errors.New("mail is not configured")
	}
	return s.mailer.Send(mailer.Message{To: to, Subject: subject, Text: text})
}

// verificationURL and resetURL point at the single-page app, which reads the
// token and calls the API. The token never appears in a server-side redirect.
func (s *Server) verifyURL(token string) string {
	return s.cfg.Issuer + "/verify?token=" + token
}

func (s *Server) resetURL(token string) string {
	return s.cfg.Issuer + "/reset?token=" + token
}

func (s *Server) sendVerification(r *http.Request, u *store.User) {
	token, err := s.st.CreateEmailToken(r.Context(), u.ID, store.EmailVerify, store.EmailLinkTTL)
	if err != nil {
		s.log.Error("create verification token", "err", err)
		return
	}
	text := "你好 " + displayNameOr(u) + "：\n\n" +
		"请打开下面的链接确认你的邮箱地址。链接 30 分钟内有效，只能使用一次。\n\n" +
		s.verifyURL(token) + "\n\n" +
		"如果这不是你的操作，可以忽略这封信。\n\nXAuth"
	if err := s.sendMail(u.Email, "确认你的 XAuth 邮箱", text); err != nil {
		s.log.Error("send verification mail", "err", err)
	}
}

func (s *Server) sendPasswordReset(r *http.Request, u *store.User) {
	token, err := s.st.CreateEmailToken(r.Context(), u.ID, store.EmailReset, store.EmailLinkTTL)
	if err != nil {
		s.log.Error("create reset token", "err", err)
		return
	}
	text := "你好 " + displayNameOr(u) + "：\n\n" +
		"有人请求重置你的 XAuth 密码。打开下面的链接设置新密码。链接 30 分钟内有效，只能使用一次。\n\n" +
		s.resetURL(token) + "\n\n" +
		"如果不是你发起的，请忽略这封信，你的密码不会改变。\n\nXAuth"
	if err := s.sendMail(u.Email, "重置你的 XAuth 密码", text); err != nil {
		s.log.Error("send reset mail", "err", err)
	}
}

func displayNameOr(u *store.User) string {
	if strings.TrimSpace(u.Name) != "" {
		return u.Name
	}
	return u.Email
}

// handleRequestVerify sends a verification link to the signed-in user.
// It is a no-op when the address is already confirmed or mail is off.
func (s *Server) handleRequestVerify(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.cfg.SMTP.Enabled() {
		writeError(w, http.StatusConflict, "mail_not_configured")
		return
	}
	if !info.user.EmailVerified {
		s.sendVerification(r, info.user)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleVerify consumes a verification link. The token comes from the SPA.
func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "invalid_link")
		return
	}
	userID, err := s.st.ConsumeEmailToken(r.Context(), req.Token, store.EmailVerify)
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	if err := s.st.SetEmailVerified(r.Context(), userID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvEmailVerify, ActorID: userID, IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleForgotPassword always answers the same way, whether or not the
// account exists, so the endpoint cannot be used to probe for addresses.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.SMTP.Enabled() {
		writeError(w, http.StatusConflict, "mail_not_configured")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	email := store.NormalizeEmail(req.Email)
	if !s.resetIP.allowed(s.clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	s.resetIP.hit(s.clientIP(r))
	u, err := s.st.UserByEmail(r.Context(), email)
	if err == nil && !u.Disabled {
		s.sendPasswordReset(r, u)
		s.audit(r.Context(), store.Event{Type: store.EvPasswordResetRequest, ActorID: u.ID, IP: s.clientIP(r), Detail: map[string]any{"email": email}})
	} else if errors.Is(err, store.ErrNotFound) {
		s.audit(r.Context(), store.Event{Type: store.EvPasswordResetRequest, IP: s.clientIP(r), Detail: map[string]any{"email": email}})
	} else if err != nil {
		s.log.Error("lookup for reset", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleResetPassword sets a new password from a one-time link, then ends
// every other session and revokes the user's tokens.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "invalid_link")
		return
	}
	if err := password.Validate(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, passwordError(err))
		return
	}
	hash, err := password.Hash(req.Password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	userID, err := s.st.ConsumeEmailToken(r.Context(), req.Token, store.EmailReset)
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	if err := s.st.SetPasswordHash(r.Context(), userID, hash); err != nil {
		s.internalError(w, r, err)
		return
	}
	u, err := s.st.UserByID(r.Context(), userID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.st.RevokeUserAccess(r.Context(), userID, ""); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvPasswordReset, ActorID: userID, IP: s.clientIP(r)})
	if _, err := s.startSession(w, r, u.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserJSON(u)})
}

func (s *Server) writeLinkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrLinkUsed):
		writeError(w, http.StatusGone, "link_used")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusGone, "link_expired")
	default:
		writeError(w, http.StatusInternalServerError, "internal")
	}
}
