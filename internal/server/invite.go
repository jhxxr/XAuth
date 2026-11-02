package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xauth/internal/store"
)

// Registration invite defaults.
const (
	defaultInviteDays = 7
	maxInviteDays     = 90
)

// defaultRegistration is the policy used before an admin changes it. Open
// registration is off unless XAUTH_SIGNUP opts in; invites are the default.
func (s *Server) defaultRegistration() store.RegistrationSettings {
	mode := store.RegistrationInvite
	if s.cfg.SignupEnabled {
		mode = store.RegistrationOpen
	}
	return store.RegistrationSettings{Mode: mode}
}

// registration reads the stored policy, falling back to the config default.
func (s *Server) registration(ctx context.Context) store.RegistrationSettings {
	r, err := s.st.Registration(ctx, s.defaultRegistration())
	if err != nil {
		s.log.Error("read registration settings", "err", err)
		return s.defaultRegistration()
	}
	return r
}

func (s *Server) inviteURL(code string) string {
	return s.cfg.Issuer + "/register?invite=" + url.QueryEscape(code)
}

func (s *Server) sendInviteMail(to, code, link, note string, expires time.Time) error {
	text := "你好：\n\n管理员邀请你注册 XAuth 账户。\n\n" +
		"邀请码：" + code + "\n" +
		"注册链接：" + link + "\n\n"
	if note != "" {
		text += "留言：" + note + "\n\n"
	}
	text += "邀请码在 " + expires.UTC().Format("2006-01-02 15:04") + "（UTC）前有效，只能使用一次。\n\nXAuth"
	return s.sendMail(to, "邀请你注册 XAuth", text)
}

type inviteJSON struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Note        string     `json:"note"`
	Status      string     `json:"status"` // pending, used, expired or revoked
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	UsedAt      *time.Time `json:"usedAt"`
	UsedByEmail string     `json:"usedByEmail"`
	RevokedAt   *time.Time `json:"revokedAt"`
}

func toInviteJSON(inv *store.Invite, now time.Time) inviteJSON {
	out := inviteJSON{
		ID: inv.ID, Email: inv.Email, Note: inv.Note, CreatedAt: inv.CreatedAt,
		ExpiresAt: inv.ExpiresAt, UsedByEmail: inv.UsedByEmail,
	}
	if !inv.UsedAt.IsZero() {
		t := inv.UsedAt
		out.UsedAt = &t
	}
	if !inv.RevokedAt.IsZero() {
		t := inv.RevokedAt
		out.RevokedAt = &t
	}
	switch {
	case !inv.RevokedAt.IsZero():
		out.Status = "revoked"
	case !inv.UsedAt.IsZero():
		out.Status = "used"
	case !inv.ExpiresAt.After(now):
		out.Status = "expired"
	default:
		out.Status = "pending"
	}
	return out
}

func (s *Server) handleAdminListInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.st.ListInvites(r.Context(), 200)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := s.st.Now()
	out := make([]inviteJSON, 0, len(invites))
	for _, inv := range invites {
		out = append(out, toInviteJSON(inv, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": out})
}

type createInviteRequest struct {
	Email         string `json:"email"`
	Note          string `json:"note"`
	ExpiresInDays int    `json:"expiresInDays"`
	SendEmail     bool   `json:"sendEmail"`
}

type createInviteResponse struct {
	Invite  inviteJSON `json:"invite"`
	Code    string     `json:"code"`
	Link    string     `json:"link"`
	Emailed bool       `json:"emailed"`
}

// handleAdminCreateInvite mints a code and returns it once; only its hash
// is stored. When an address is given the invite is reserved for it, and
// the mail is sent if the admin asked for it and SMTP is configured.
func (s *Server) handleAdminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var req createInviteRequest
	if !decode(w, r, &req) {
		return
	}
	email := ""
	if strings.TrimSpace(req.Email) != "" {
		var ok bool
		if email, ok = validEmail(req.Email); !ok {
			writeError(w, http.StatusBadRequest, "invalid_email")
			return
		}
	}
	note, ok := cleanName(req.Note, 0, 200)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_note")
		return
	}
	days := req.ExpiresInDays
	if days == 0 {
		days = defaultInviteDays
	}
	if days < 1 || days > maxInviteDays {
		writeError(w, http.StatusBadRequest, "invalid_expiry")
		return
	}
	if req.SendEmail && email != "" && !s.cfg.SMTP.Enabled() {
		writeError(w, http.StatusConflict, "mail_not_configured")
		return
	}
	code := store.NewInviteCode()
	inv := &store.Invite{
		Email: email, Note: note, CreatedBy: sessionFrom(r.Context()).user.ID,
		ExpiresAt: s.st.Now().Add(time.Duration(days) * 24 * time.Hour),
	}
	if err := s.st.CreateInvite(r.Context(), inv, code); err != nil {
		s.internalError(w, r, err)
		return
	}
	link := s.inviteURL(code)
	emailed := false
	if req.SendEmail && email != "" {
		if err := s.sendInviteMail(email, code, link, note, inv.ExpiresAt); err != nil {
			// The invite still exists; tell the admin to share it manually.
			s.log.Error("send invite mail", "err", err)
		} else {
			emailed = true
		}
	}
	s.audit(r.Context(), store.Event{
		Type: store.EvInviteCreate, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "invite", SubjectID: inv.ID, SubjectName: email,
		IP: s.clientIP(r), Detail: map[string]any{"email": email, "emailed": emailed},
	})
	writeJSON(w, http.StatusCreated, createInviteResponse{
		Invite: toInviteJSON(inv, s.st.Now()), Code: code, Link: link, Emailed: emailed,
	})
}

func (s *Server) handleAdminRevokeInvite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := s.st.RevokeInvite(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, store.ErrInviteInvalid):
		writeError(w, http.StatusConflict, "invite_not_pending")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvInviteRevoke, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "invite", SubjectID: id, IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleInviteInfo lets the register page check a code before showing the
// form, and reveals the address the invite is reserved for (if any).
// It is rate limited so codes cannot be enumerated.
func (s *Server) handleInviteInfo(w http.ResponseWriter, r *http.Request) {
	code := store.NormalizeInviteCode(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid_invite")
		return
	}
	ip := s.clientIP(r)
	if !s.inviteIP.allowed(ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	s.inviteIP.hit(ip)
	inv, err := s.st.InviteByCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "invalid_invite")
			return
		}
		s.internalError(w, r, err)
		return
	}
	now := s.st.Now()
	switch {
	case !inv.RevokedAt.IsZero() || !inv.UsedAt.IsZero():
		writeError(w, http.StatusGone, "invalid_invite")
		return
	case !inv.ExpiresAt.After(now):
		writeError(w, http.StatusGone, "invite_expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"invite": map[string]any{"email": inv.Email, "expiresAt": inv.ExpiresAt},
	})
}

type registrationSettingsJSON struct {
	Mode              string `json:"mode"`
	InviteVerifyEmail bool   `json:"inviteVerifyEmail"`
	MailEnabled       bool   `json:"mailEnabled"`
}

func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	reg := s.registration(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"registration": registrationSettingsJSON{
			Mode: reg.Mode, InviteVerifyEmail: reg.InviteVerifyEmail, MailEnabled: s.cfg.SMTP.Enabled(),
		},
		"inviteDefaults": map[string]int{"expiresInDays": defaultInviteDays, "maxExpiresInDays": maxInviteDays},
	})
}

type updateSettingsRequest struct {
	Mode              *string `json:"mode"`
	InviteVerifyEmail *bool   `json:"inviteVerifyEmail"`
}

func (s *Server) handleAdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if !decode(w, r, &req) {
		return
	}
	reg := s.registration(r.Context())
	changed := map[string]any{}
	if req.Mode != nil {
		if !store.ValidRegistrationMode(*req.Mode) {
			writeError(w, http.StatusBadRequest, "invalid_mode")
			return
		}
		reg.Mode = *req.Mode
		changed["mode"] = reg.Mode
	}
	if req.InviteVerifyEmail != nil {
		if *req.InviteVerifyEmail && !s.cfg.SMTP.Enabled() && !reg.InviteVerifyEmail {
			writeError(w, http.StatusConflict, "mail_not_configured")
			return
		}
		reg.InviteVerifyEmail = *req.InviteVerifyEmail
		changed["inviteVerifyEmail"] = reg.InviteVerifyEmail
	}
	if len(changed) > 0 {
		if err := s.st.SetRegistration(r.Context(), reg); err != nil {
			s.internalError(w, r, err)
			return
		}
		s.audit(r.Context(), store.Event{Type: store.EvSettingsUpdate, ActorID: sessionFrom(r.Context()).user.ID,
			IP: s.clientIP(r), Detail: changed})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"registration": registrationSettingsJSON{
			Mode: reg.Mode, InviteVerifyEmail: reg.InviteVerifyEmail, MailEnabled: s.cfg.SMTP.Enabled(),
		},
	})
}
