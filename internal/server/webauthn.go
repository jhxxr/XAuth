package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"xauth/internal/store"
)

const webauthnChallengeTTL = 5 * time.Minute

type passkeyJSON struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func passkeyToJSON(c store.WebAuthnCredential) passkeyJSON {
	out := passkeyJSON{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt}
	if !c.LastUsedAt.IsZero() {
		t := c.LastUsedAt
		out.LastUsedAt = &t
	}
	return out
}

// webauthnConfig builds the relying-party configuration from the issuer.
func (s *Server) webauthnRP() (*webauthn.WebAuthn, error) {
	u, err := url.Parse(s.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	rpID := u.Hostname()
	// WebAuthn RP IDs must be domain names. In development the issuer is
	// often an IP; localhost is the one host browsers treat as secure there.
	if ip := net.ParseIP(rpID); ip != nil && ip.IsLoopback() {
		rpID = "localhost"
	}
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "XAuth",
		RPOrigins:     []string{s.cfg.Issuer},
	})
}

func (s *Server) webauthnUser(r *http.Request, userID string) (store.WebAuthnUser, error) {
	u, err := s.st.UserByID(r.Context(), userID)
	if err != nil {
		return store.WebAuthnUser{}, err
	}
	creds, err := s.st.UserWebAuthnCredentials(r.Context(), userID)
	if err != nil {
		return store.WebAuthnUser{}, err
	}
	return store.WebAuthnUser{User: u, Credentials: creds}, nil
}

// handleListPasskeys returns the signed-in user's passkeys.
func (s *Server) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	creds, err := s.st.UserWebAuthnCredentials(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]passkeyJSON, 0, len(creds))
	for _, c := range creds {
		out = append(out, passkeyToJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": out, "hasPassword": info.user.PasswordHash != ""})
}

// handlePasskeyRegisterBegin starts a registration ceremony for the signed-in user.
func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	wa, err := s.webauthnRP()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	user, err := s.webauthnUser(r, info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	creation, session, err := wa.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
		}),
		webauthn.WithExclusions(credentialDescriptors(user.Credentials)),
	)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	id, err := s.saveChallenge(r, session)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challengeId": id, "options": creation,
		"name": strings.TrimSpace(req.Name),
	})
}

// handlePasskeyRegisterFinish verifies the attestation and stores the passkey.
func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	session, err := s.takeChallenge(r, id)
	if err != nil {
		s.writeChallengeError(w)
		return
	}
	wa, err := s.webauthnRP()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	user, err := s.webauthnUser(r, info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cred, err := wa.FinishRegistration(user, session, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "passkey_failed")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "通行密钥"
	}
	saved, err := s.st.AddWebAuthnCredential(r.Context(), info.user.ID, name, cred)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvPasskeyAdd, ActorID: info.user.ID, SubjectType: "passkey", SubjectID: saved.ID, SubjectName: saved.Name})
	writeJSON(w, http.StatusCreated, map[string]any{"passkey": passkeyToJSON(*saved)})
}

// handlePasskeyLoginBegin starts a discoverable login. It reveals nothing
// about accounts: the browser picks a credential and only a successful
// assertion identifies the user.
func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	wa, err := s.webauthnRP()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	assertion, session, err := wa.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationPreferred),
	)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	id, err := s.saveChallenge(r, session)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"challengeId": id, "options": assertion})
}

// handlePasskeyLoginFinish verifies the assertion and starts a browser session.
func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	challengeID := chi.URLParam(r, "id")
	session, err := s.takeChallenge(r, challengeID)
	if err != nil {
		s.writeChallengeError(w)
		return
	}
	wa, err := s.webauthnRP()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		row, u, err := s.st.FindWebAuthnCredential(r.Context(), rawID)
		if err != nil {
			return nil, err
		}
		if u.Disabled {
			return nil, errors.New("account disabled")
		}
		creds, err := s.st.UserWebAuthnCredentials(r.Context(), u.ID)
		if err != nil {
			return nil, err
		}
		return store.WebAuthnUser{User: u, Credentials: creds, Current: row}, nil
	}
	user, cred, err := wa.FinishPasskeyLogin(handler, session, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "passkey_failed")
		return
	}
	wu, _ := user.(store.WebAuthnUser)
	if wu.User == nil {
		writeError(w, http.StatusUnauthorized, "passkey_failed")
		return
	}
	if rowID := wu.CurrentRowID(); rowID != "" {
		_ = s.st.UpdateWebAuthnCredential(r.Context(), rowID, cred)
	}
	_ = s.st.TouchLogin(r.Context(), wu.User.ID)
	wu.User.LastLoginAt = s.st.Now()
	sess, err := s.startSession(w, r, wu.User.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvLoginSuccess, ActorID: wu.User.ID, IP: s.clientIP(r), Detail: map[string]any{"amr": "webauthn"}})
	resp := authResponse{User: toUserJSON(wu.User)}
	flow := r.URL.Query().Get("flow")
	if flow != "" {
		if a, err := s.st.AuthRequestByID(r.Context(), flow); err != nil || a.Status != store.AuthPending {
			resp.FlowError = "flow_expired"
		} else if s.approveAppWithAMR(r.Context(), a, wu.User.ID, sess.AuthTime, webauthnAMR) {
			resp.Redirect = callbackURL(flow)
		} else {
			resp.FlowError = "access_denied"
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDeletePasskey removes one of the user's passkeys. A password account
// always keeps a way in; a passwordless one may not delete its last passkey.
func (s *Server) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	creds, err := s.st.UserWebAuthnCredentials(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if len(creds) <= 1 && info.user.PasswordHash == "" {
		writeError(w, http.StatusConflict, "last_login_method")
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.st.DeleteWebAuthnCredential(r.Context(), info.user.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvPasskeyRemove, ActorID: info.user.ID, SubjectType: "passkey", SubjectID: id})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) saveChallenge(r *http.Request, session *webauthn.SessionData) (string, error) {
	data, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	id := store.NewID()
	return id, s.st.SaveWebAuthnChallenge(r.Context(), id, string(data), webauthnChallengeTTL)
}

func (s *Server) takeChallenge(r *http.Request, id string) (webauthn.SessionData, error) {
	var session webauthn.SessionData
	raw, err := s.st.TakeWebAuthnChallenge(r.Context(), id)
	if err != nil {
		return session, err
	}
	err = json.Unmarshal([]byte(raw), &session)
	return session, err
}

func (s *Server) writeChallengeError(w http.ResponseWriter) {
	writeError(w, http.StatusGone, "passkey_expired")
}

func credentialDescriptors(creds []store.WebAuthnCredential) []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(creds))
	for _, c := range creds {
		out = append(out, c.Data.Descriptor())
	}
	return out
}
