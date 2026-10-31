package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"xauth/internal/password"
	"xauth/internal/store"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]*userJSON, 0, len(users))
	for _, u := range users {
		out = append(out, toUserJSON(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type createUserRequest struct {
	Email         string `json:"email"`
	Name          string `json:"name"`
	Password      string `json:"password"`
	IsAdmin       bool   `json:"isAdmin"`
	EmailVerified bool   `json:"emailVerified"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if !decode(w, r, &req) {
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
	hash, err := password.Hash(req.Password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	u := &store.User{Email: email, Name: name, PasswordHash: hash, IsAdmin: req.IsAdmin, EmailVerified: req.EmailVerified}
	if err := s.st.CreateUser(r.Context(), u); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "email_taken")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvUserCreate, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "user", SubjectID: u.ID, IP: s.clientIP(r), Detail: map[string]any{"isAdmin": u.IsAdmin}})
	writeJSON(w, http.StatusCreated, map[string]any{"user": toUserJSON(u)})
}

type updateUserRequest struct {
	Name          *string `json:"name"`
	IsAdmin       *bool   `json:"isAdmin"`
	Disabled      *bool   `json:"disabled"`
	EmailVerified *bool   `json:"emailVerified"`
	Password      *string `json:"password"`
}

// handleUpdateUser edits another account. Disabling a user or setting a new
// password signs them out everywhere and revokes their tokens.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	u, err := s.st.UserByID(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var req updateUserRequest
	if !decode(w, r, &req) {
		return
	}
	self := u.ID == me.user.ID
	if self && ((req.IsAdmin != nil && !*req.IsAdmin) || (req.Disabled != nil && *req.Disabled)) {
		writeError(w, http.StatusBadRequest, "cannot_change_self")
		return
	}
	wasAdmin := u.IsAdmin
	revoke := false
	changed := map[string]any{}
	if req.Name != nil {
		name, ok := cleanName(*req.Name, 0, 64)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_name")
			return
		}
		u.Name = name
	}
	if req.IsAdmin != nil {
		u.IsAdmin = *req.IsAdmin
		changed["isAdmin"] = u.IsAdmin
	}
	if req.Disabled != nil {
		revoke = revoke || (*req.Disabled && !u.Disabled)
		u.Disabled = *req.Disabled
		changed["disabled"] = u.Disabled
	}
	if req.EmailVerified != nil {
		u.EmailVerified = *req.EmailVerified
		changed["emailVerified"] = u.EmailVerified
	}
	if req.Password != nil {
		if err := password.Validate(*req.Password); err != nil {
			writeError(w, http.StatusBadRequest, passwordError(err))
			return
		}
		hash, err := password.Hash(*req.Password)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		u.PasswordHash = hash
		revoke = true
		changed["password"] = true
	}
	if err := s.st.UpdateUser(r.Context(), u); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			writeError(w, http.StatusConflict, "last_admin")
			return
		}
		s.internalError(w, r, err)
		return
	}
	if revoke {
		keep := ""
		if self {
			keep = me.sess.TokenHash
		}
		if err := s.st.RevokeUserAccess(r.Context(), u.ID, keep); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	if wasAdmin && !u.IsAdmin {
		if err := s.st.RevokeAdminOnlyGrants(r.Context(), u.ID); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	s.audit(r.Context(), store.Event{Type: store.EvUserUpdate, ActorID: me.user.ID, SubjectType: "user", SubjectID: u.ID,
		IP: s.clientIP(r), Detail: changed})
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserJSON(u)})
}

type appJSON struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	Type                   string    `json:"type"`
	RedirectURIs           []string  `json:"redirectUris"`
	PostLogoutRedirectURIs []string  `json:"postLogoutRedirectUris"`
	AllowRefresh           bool      `json:"allowRefresh"`
	Access                 string    `json:"access"`
	AllowedUserIDs         []string  `json:"allowedUserIds"`
	HasSecret              bool      `json:"hasSecret"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func toAppJSON(c *store.Client, allowed []string) appJSON {
	return appJSON{
		ID: c.ID, Name: c.Name, Type: c.Type,
		RedirectURIs: nonNil(c.RedirectURIs), PostLogoutRedirectURIs: nonNil(c.PostLogoutRedirectURIs),
		AllowRefresh: c.AllowRefresh, Access: c.Access, AllowedUserIDs: nonNil(allowed),
		HasSecret: c.SecretHash != "", CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func validAccess(v string) bool {
	return v == store.AccessAll || v == store.AccessAdmins || v == store.AccessSelected
}

type endpointsJSON struct {
	Issuer        string `json:"issuer"`
	Discovery     string `json:"discovery"`
	Authorization string `json:"authorization"`
	Token         string `json:"token"`
	Userinfo      string `json:"userinfo"`
	JWKS          string `json:"jwks"`
	EndSession    string `json:"endSession"`
	Revocation    string `json:"revocation"`
	Introspection string `json:"introspection"`
}

func (s *Server) endpoints() endpointsJSON {
	o, iss := s.provider.OP, s.cfg.Issuer
	return endpointsJSON{
		Issuer:        iss,
		Discovery:     iss + "/.well-known/openid-configuration",
		Authorization: o.AuthorizationEndpoint().Absolute(iss),
		Token:         o.TokenEndpoint().Absolute(iss),
		Userinfo:      o.UserinfoEndpoint().Absolute(iss),
		JWKS:          o.KeysEndpoint().Absolute(iss),
		EndSession:    o.EndSessionEndpoint().Absolute(iss),
		Revocation:    o.RevocationEndpoint().Absolute(iss),
		Introspection: o.IntrospectionEndpoint().Absolute(iss),
	}
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	clients, err := s.st.ListClients(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]appJSON, 0, len(clients))
	for _, c := range clients {
		allowed, err := s.st.ClientUserIDs(r.Context(), c.ID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		out = append(out, toAppJSON(c, allowed))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "endpoints": s.endpoints()})
}

type createAppRequest struct {
	Name                   string   `json:"name"`
	Type                   string   `json:"type"`
	RedirectURIs           []string `json:"redirectUris"`
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectUris"`
	AllowRefresh           *bool    `json:"allowRefresh"`
	Access                 string   `json:"access"`
	AllowedUserIDs         []string `json:"allowedUserIds"`
}

type createAppResponse struct {
	App    appJSON `json:"app"`
	Secret string  `json:"secret,omitempty"`
}

// redirectLists validates both URI lists and writes the error if one fails.
func (s *Server) redirectLists(w http.ResponseWriter, clientType string, redirects, logouts []string) ([]string, []string, bool) {
	r, reason := validateRedirectURIs(redirects, clientType, s.cfg.Dev, true)
	if reason != "" {
		writeErrorMessage(w, http.StatusBadRequest, "invalid_redirect_uri", "回调地址无效。"+reason)
		return nil, nil, false
	}
	l, reason := validateRedirectURIs(logouts, clientType, s.cfg.Dev, false)
	if reason != "" {
		writeErrorMessage(w, http.StatusBadRequest, "invalid_redirect_uri", "退出后跳转地址无效。"+reason)
		return nil, nil, false
	}
	return r, l, true
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var req createAppRequest
	if !decode(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name, 1, 64)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_app_name")
		return
	}
	switch req.Type {
	case store.ClientWeb, store.ClientSPA, store.ClientNative:
	default:
		writeError(w, http.StatusBadRequest, "invalid_app_type")
		return
	}
	redirects, logouts, ok := s.redirectLists(w, req.Type, req.RedirectURIs, req.PostLogoutRedirectURIs)
	if !ok {
		return
	}
	access := req.Access
	if access == "" {
		access = store.AccessAll
	}
	if !validAccess(access) {
		writeError(w, http.StatusBadRequest, "invalid_access")
		return
	}
	c := &store.Client{
		Name: name, Type: req.Type, RedirectURIs: redirects, PostLogoutRedirectURIs: logouts,
		AllowRefresh: req.AllowRefresh == nil || *req.AllowRefresh, Access: access,
	}
	secret, err := s.st.CreateClient(r.Context(), c)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if access == store.AccessSelected {
		if err := s.st.SetClientUsers(r.Context(), c.ID, req.AllowedUserIDs); err != nil {
			_ = s.st.DeleteClient(r.Context(), c.ID)
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusBadRequest, "unknown_user")
				return
			}
			s.internalError(w, r, err)
			return
		}
	}
	s.audit(r.Context(), store.Event{Type: store.EvAppCreate, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "app", SubjectID: c.ID, IP: s.clientIP(r), Detail: map[string]any{"type": c.Type, "access": access}})
	writeJSON(w, http.StatusCreated, createAppResponse{App: toAppJSON(c, req.AllowedUserIDs), Secret: secret})
}

type updateAppRequest struct {
	Name                   *string   `json:"name"`
	RedirectURIs           *[]string `json:"redirectUris"`
	PostLogoutRedirectURIs *[]string `json:"postLogoutRedirectUris"`
	AllowRefresh           *bool     `json:"allowRefresh"`
	Access                 *string   `json:"access"`
	AllowedUserIDs         *[]string `json:"allowedUserIds"`
}

func (s *Server) loadApp(w http.ResponseWriter, r *http.Request) (*store.Client, bool) {
	c, err := s.st.ClientByID(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return c, true
}

func (s *Server) handleUpdateApp(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	var req updateAppRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil {
		name, ok := cleanName(*req.Name, 1, 64)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_app_name")
			return
		}
		c.Name = name
	}
	redirects, logouts := c.RedirectURIs, c.PostLogoutRedirectURIs
	if req.RedirectURIs != nil {
		redirects = *req.RedirectURIs
	}
	if req.PostLogoutRedirectURIs != nil {
		logouts = *req.PostLogoutRedirectURIs
	}
	if c.RedirectURIs, c.PostLogoutRedirectURIs, ok = s.redirectLists(w, c.Type, redirects, logouts); !ok {
		return
	}
	if req.AllowRefresh != nil {
		c.AllowRefresh = *req.AllowRefresh
	}
	if req.Access != nil {
		if !validAccess(*req.Access) {
			writeError(w, http.StatusBadRequest, "invalid_access")
			return
		}
		c.Access = *req.Access
	}
	if err := s.st.UpdateClient(r.Context(), c); err != nil {
		s.internalError(w, r, err)
		return
	}
	allowed, err := s.st.ClientUserIDs(r.Context(), c.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if c.Access == store.AccessSelected && req.AllowedUserIDs != nil {
		if err := s.st.SetClientUsers(r.Context(), c.ID, *req.AllowedUserIDs); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusBadRequest, "unknown_user")
				return
			}
			s.internalError(w, r, err)
			return
		}
		allowed = nonNil(*req.AllowedUserIDs)
	}
	if err := s.st.RevokeDisallowed(r.Context(), c.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvAppUpdate, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "app", SubjectID: c.ID, IP: s.clientIP(r), Detail: map[string]any{"access": c.Access}})
	writeJSON(w, http.StatusOK, map[string]any{"app": toAppJSON(c, allowed)})
}

func (s *Server) handleRotateSecret(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadApp(w, r)
	if !ok {
		return
	}
	if !c.Confidential() {
		writeError(w, http.StatusBadRequest, "not_confidential")
		return
	}
	secret, err := s.st.RotateClientSecret(r.Context(), c.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvAppSecret, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "app", SubjectID: c.ID, IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret})
}

func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	err := s.st.DeleteClient(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvAppDelete, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "app", SubjectID: chi.URLParam(r, "id"), IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
