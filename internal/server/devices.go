package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"xauth/internal/store"
)

type sessionJSONItem struct {
	ID        string    `json:"id"`
	UserAgent string    `json:"userAgent"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
	Current   bool      `json:"current"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.st.ListSessions(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]sessionJSONItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionJSONItem{
			ID: row.ID, UserAgent: row.UserAgent, IP: row.IP,
			CreatedAt: row.CreatedAt, LastSeen: row.LastSeen, Current: row.TokenHash == info.sess.TokenHash,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	err := s.st.DeleteUserSession(r.Context(), info.user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if id == info.sess.ID {
		s.clearCookie(w)
	}
	s.audit(r.Context(), store.Event{Type: store.EvSessionRevoke, ActorID: info.user.ID, SubjectType: "session", SubjectID: id,
		IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	n, err := s.st.DeleteOtherSessions(r.Context(), info.user.ID, info.sess.TokenHash)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if n > 0 {
		s.audit(r.Context(), store.Event{Type: store.EvSessionRevoke, ActorID: info.user.ID, IP: s.clientIP(r),
			Detail: map[string]any{"others": n}})
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": n})
}

type grantJSON struct {
	ClientID   string    `json:"clientId"`
	ClientName string    `json:"clientName"`
	Scopes     []string  `json:"scopes"`
	LastUsed   time.Time `json:"lastUsed"`
	Refresh    bool      `json:"refresh"`
}

func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.st.ListGrants(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]grantJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, grantJSON{ClientID: row.ClientID, ClientName: row.ClientName, Scopes: nonNil(row.Scopes), LastUsed: row.LastUsed, Refresh: row.Refresh})
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}

func (s *Server) handleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	clientID := chi.URLParam(r, "clientID")
	if _, err := s.st.ClientByID(r.Context(), clientID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.st.RevokeGrant(r.Context(), info.user.ID, clientID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvGrantRevoke, ActorID: info.user.ID, SubjectType: "app", SubjectID: clientID, IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleForceSignOut ends every session and token of a user. Admins cannot
// target themselves; they sign out from their own account page.
func (s *Server) handleForceSignOut(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	id := chi.URLParam(r, "id")
	if id == me.user.ID {
		writeError(w, http.StatusBadRequest, "cannot_change_self")
		return
	}
	if _, err := s.st.UserByID(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.st.RevokeUserAccess(r.Context(), id, ""); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvUserSignout, ActorID: me.user.ID, SubjectType: "user", SubjectID: id, IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
