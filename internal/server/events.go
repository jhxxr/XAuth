package server

import (
	"net/http"
	"strconv"
	"time"

	"xauth/internal/store"
)

type eventJSON struct {
	ID          int64          `json:"id"`
	At          time.Time      `json:"at"`
	Type        string         `json:"type"`
	ActorID     string         `json:"actorId,omitempty"`
	ActorEmail  string         `json:"actorEmail,omitempty"`
	SubjectType string         `json:"subjectType,omitempty"`
	SubjectID   string         `json:"subjectId,omitempty"`
	SubjectName string         `json:"subjectName,omitempty"`
	IP          string         `json:"ip,omitempty"`
	Detail      map[string]any `json:"detail,omitempty"`
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.EventFilter{UserID: q.Get("user")}
	if group := q.Get("group"); group != "" {
		types, ok := store.EventGroups[group]
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_group")
			return
		}
		f.Types = types
	}
	if v := q.Get("before"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		f.BeforeID = id
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if n > 100 {
			n = 100
		}
		f.Limit = n
	}
	rows, err := s.st.ListEvents(r.Context(), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]eventJSON, 0, len(rows))
	for _, e := range rows {
		out = append(out, eventJSON{ID: e.ID, At: e.At, Type: e.Type, ActorID: e.ActorID, ActorEmail: e.ActorEmail,
			SubjectType: e.SubjectType, SubjectID: e.SubjectID, SubjectName: e.SubjectName, IP: e.IP, Detail: e.Detail})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}
