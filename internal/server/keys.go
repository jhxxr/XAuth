package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"xauth/internal/store"
)

type keyJSON struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"createdAt"`
	Current   bool       `json:"current"`
	RetiredAt *time.Time `json:"retiredAt"`
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.AllSigningKeys(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var current string
	for _, k := range rows {
		if k.RetiredAt.IsZero() {
			current = k.ID
			break
		}
	}
	out := make([]keyJSON, 0, len(rows))
	for _, k := range rows {
		item := keyJSON{ID: k.ID, CreatedAt: k.CreatedAt, Current: k.ID == current}
		if !k.RetiredAt.IsZero() {
			t := k.RetiredAt
			item.RetiredAt = &t
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	k, err := s.provider.Storage.RotateKey(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvKeyRotate, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "key", SubjectID: k.ID, IP: s.clientIP(r)})
	writeJSON(w, http.StatusCreated, map[string]any{"key": keyJSON{ID: k.ID, CreatedAt: k.CreatedAt, Current: true}})
}

func (s *Server) handleRetireKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := s.st.SigningKeys(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var found bool
	for _, k := range rows {
		if k.ID == id {
			found = true
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if len(rows) < 2 {
		writeError(w, http.StatusConflict, "last_key")
		return
	}
	if err := s.st.RetireSigningKey(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvKeyRetire, ActorID: sessionFrom(r.Context()).user.ID,
		SubjectType: "key", SubjectID: id, IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
