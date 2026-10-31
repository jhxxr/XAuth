package server

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"xauth/internal/store"
	"xauth/internal/totp"
)

const totpInputLimit = 2048

type totpJSON struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Issuer      string    `json:"issuer"`
	AccountName string    `json:"accountName"`
	Digits      int       `json:"digits"`
	Period      int       `json:"period"`
	Algorithm   string    `json:"algorithm"`
	Note        string    `json:"note"`
	Shared      bool      `json:"shared"`
	UserIDs     []string  `json:"userIds,omitempty"`
	Code        string    `json:"code,omitempty"`
	Remaining   int       `json:"remaining,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type totpInput struct {
	Input       string   `json:"input"`
	Name        string   `json:"name"`
	Issuer      *string  `json:"issuer"`
	AccountName *string  `json:"accountName"`
	Secret      string   `json:"secret"`
	Digits      int      `json:"digits"`
	Period      int      `json:"period"`
	Algorithm   string   `json:"algorithm"`
	Note        *string  `json:"note"`
	UserIDs     []string `json:"userIds"`
}

func (in totpInput) entry() (totp.Entry, error) {
	return totp.Parse(in.Input, totp.Entry{
		Secret: in.Secret, Name: in.Name, Issuer: textOf(in.Issuer), AccountName: textOf(in.AccountName),
		Digits: in.Digits, Period: in.Period, Algorithm: in.Algorithm, Note: textOf(in.Note),
	})
}

func textOf(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func totpToJSON(e store.TotpEntry) totpJSON {
	ids := e.UserIDs
	if ids == nil {
		ids = []string{}
	}
	return totpJSON{
		ID: e.ID, Name: e.Name, Issuer: e.Issuer, AccountName: e.AccountName,
		Digits: e.Digits, Period: e.Period, Algorithm: e.Algorithm, Note: e.Note,
		Shared: e.Shared, UserIDs: ids, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func totpCodeJSON(c store.TotpCode) totpJSON {
	out := totpToJSON(c.Entry)
	out.Code = c.Code
	out.Remaining = c.Remaining
	out.UserIDs = nil
	return out
}

func (s *Server) handleListAdminTotp(w http.ResponseWriter, r *http.Request) {
	codes, err := s.st.SharedTotpCodes(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]totpJSON, 0, len(codes))
	for _, c := range codes {
		item := totpToJSON(c.Entry)
		item.Code = c.Code
		item.Remaining = c.Remaining
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func (s *Server) handleCreateAdminTotp(w http.ResponseWriter, r *http.Request) {
	info := sessionFrom(r.Context())
	var in totpInput
	if !decode(w, r, &in) || !s.validTotpInput(w, in, true) {
		return
	}
	parsed, err := in.entry()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_totp")
		return
	}
	e := entryFromParsed(parsed, "", info.user.ID)
	if err := s.st.CreateTotpEntry(r.Context(), e, parsed.Secret); err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.st.SetTotpGrants(r.Context(), e.ID, in.UserIDs, info.user.ID); err != nil {
		_ = s.st.DeleteTotpEntry(r.Context(), e.ID)
		s.writeTotpStoreError(w, r, err)
		return
	}
	e.UserIDs = uniqueIDs(in.UserIDs)
	e.Shared = true
	s.audit(r.Context(), store.Event{Type: store.EvTotpCreate, ActorID: info.user.ID, SubjectType: "totp", SubjectID: e.ID, SubjectName: e.Name,
		Detail: map[string]any{"shared": true, "users": len(e.UserIDs)}})
	writeJSON(w, http.StatusCreated, map[string]any{"entry": totpToJSON(*e)})
}

func (s *Server) handleUpdateAdminTotp(w http.ResponseWriter, r *http.Request) {
	info := sessionFrom(r.Context())
	current, ok := s.sharedTotp(w, r)
	if !ok {
		return
	}
	var in totpInput
	if !decode(w, r, &in) || !s.validTotpInput(w, in, false) {
		return
	}
	parsed, secret, ok := s.mergeTotp(w, current, in)
	if !ok {
		return
	}
	e := entryFromParsed(parsed, "", info.user.ID)
	e.ID = current.ID
	e.CreatedBy = current.CreatedBy
	if err := s.st.UpdateTotpEntry(r.Context(), e, secret); err != nil {
		s.writeTotpStoreError(w, r, err)
		return
	}
	if err := s.st.SetTotpGrants(r.Context(), e.ID, in.UserIDs, info.user.ID); err != nil {
		s.writeTotpStoreError(w, r, err)
		return
	}
	e.UserIDs = uniqueIDs(in.UserIDs)
	e.Shared = true
	s.audit(r.Context(), store.Event{Type: store.EvTotpUpdate, ActorID: info.user.ID, SubjectType: "totp", SubjectID: e.ID, SubjectName: e.Name,
		Detail: map[string]any{"secret": secret != "", "users": len(e.UserIDs)}})
	writeJSON(w, http.StatusOK, map[string]any{"entry": totpToJSON(*e)})
}

func (s *Server) handleDeleteAdminTotp(w http.ResponseWriter, r *http.Request) {
	info := sessionFrom(r.Context())
	current, ok := s.sharedTotp(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteTotpEntry(r.Context(), current.ID); err != nil {
		s.writeTotpStoreError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvTotpDelete, ActorID: info.user.ID, SubjectType: "totp", SubjectID: current.ID, SubjectName: current.Name, Detail: map[string]any{"shared": true}})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAssignTotp(w http.ResponseWriter, r *http.Request) {
	info := sessionFrom(r.Context())
	current, ok := s.sharedTotp(w, r)
	if !ok {
		return
	}
	var in struct {
		UserIDs []string `json:"userIds"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.st.SetTotpGrants(r.Context(), current.ID, in.UserIDs, info.user.ID); err != nil {
		s.writeTotpStoreError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvTotpAssign, ActorID: info.user.ID, SubjectType: "totp", SubjectID: current.ID, SubjectName: current.Name,
		Detail: map[string]any{"users": len(uniqueIDs(in.UserIDs))}})
	current.UserIDs = uniqueIDs(in.UserIDs)
	writeJSON(w, http.StatusOK, map[string]any{"entry": totpToJSON(*current)})
}

func (s *Server) handleListMyTotp(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	codes, err := s.st.TotpCodes(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]totpJSON, 0, len(codes))
	for _, c := range codes {
		out = append(out, totpCodeJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func (s *Server) handleCreateMyTotp(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var in totpInput
	if !decode(w, r, &in) || !s.validTotpInput(w, in, true) {
		return
	}
	parsed, err := in.entry()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_totp")
		return
	}
	e := entryFromParsed(parsed, info.user.ID, info.user.ID)
	if err := s.st.CreateTotpEntry(r.Context(), e, parsed.Secret); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvTotpCreate, ActorID: info.user.ID, SubjectType: "totp", SubjectID: e.ID, SubjectName: e.Name, Detail: map[string]any{"shared": false}})
	writeJSON(w, http.StatusCreated, map[string]any{"entry": totpToJSON(*e)})
}

func (s *Server) handleUpdateMyTotp(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	current, ok := s.ownTotp(w, r, info.user.ID)
	if !ok {
		return
	}
	var in totpInput
	if !decode(w, r, &in) || !s.validTotpInput(w, in, false) {
		return
	}
	parsed, secret, ok := s.mergeTotp(w, current, in)
	if !ok {
		return
	}
	e := entryFromParsed(parsed, info.user.ID, current.CreatedBy)
	e.ID = current.ID
	if err := s.st.UpdateTotpEntry(r.Context(), e, secret); err != nil {
		s.writeTotpStoreError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvTotpUpdate, ActorID: info.user.ID, SubjectType: "totp", SubjectID: e.ID, SubjectName: e.Name, Detail: map[string]any{"shared": false, "secret": secret != ""}})
	writeJSON(w, http.StatusOK, map[string]any{"entry": totpToJSON(*e)})
}

func (s *Server) handleDeleteMyTotp(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	current, ok := s.ownTotp(w, r, info.user.ID)
	if !ok {
		return
	}
	if err := s.st.DeleteTotpEntry(r.Context(), current.ID); err != nil {
		s.writeTotpStoreError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvTotpDelete, ActorID: info.user.ID, SubjectType: "totp", SubjectID: current.ID, SubjectName: current.Name, Detail: map[string]any{"shared": false}})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// sharedTotp loads an admin-owned entry. A personal id returns 404 so it cannot be probed.
func (s *Server) sharedTotp(w http.ResponseWriter, r *http.Request) (*store.TotpEntry, bool) {
	info := sessionFrom(r.Context())
	e, err := s.st.TotpEntryByID(r.Context(), chi.URLParam(r, "id"), info.user.ID, true)
	if err != nil {
		s.writeTotpStoreError(w, r, err)
		return nil, false
	}
	if e.OwnerUserID != "" {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	return e, true
}

func (s *Server) ownTotp(w http.ResponseWriter, r *http.Request, userID string) (*store.TotpEntry, bool) {
	e, err := s.st.TotpEntryByID(r.Context(), chi.URLParam(r, "id"), userID, false)
	if err != nil {
		s.writeTotpStoreError(w, r, err)
		return nil, false
	}
	if e.OwnerUserID != userID {
		writeError(w, http.StatusForbidden, "totp_shared")
		return nil, false
	}
	return e, true
}

func (s *Server) mergeTotp(w http.ResponseWriter, current *store.TotpEntry, in totpInput) (totp.Entry, string, bool) {
	next := totp.Entry{
		Name: current.Name, Issuer: current.Issuer, AccountName: current.AccountName,
		Digits: current.Digits, Period: current.Period, Algorithm: current.Algorithm, Note: current.Note,
	}
	var secret string
	if strings.TrimSpace(in.Input) != "" || strings.TrimSpace(in.Secret) != "" {
		parsed, err := in.entry()
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_totp")
			return totp.Entry{}, "", false
		}
		secret = parsed.Secret
		if strings.TrimSpace(in.Name) == "" {
			parsed.Name = current.Name
		}
		if in.Issuer == nil && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(in.Input)), "otpauth://") {
			parsed.Issuer = current.Issuer
		}
		if in.AccountName == nil && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(in.Input)), "otpauth://") {
			parsed.AccountName = current.AccountName
		}
		if in.Note == nil {
			parsed.Note = current.Note
		}
		return parsed, secret, true
	}
	if strings.TrimSpace(in.Name) != "" {
		next.Name = strings.TrimSpace(in.Name)
	}
	if in.Issuer != nil {
		next.Issuer = strings.TrimSpace(*in.Issuer)
	}
	if in.AccountName != nil {
		next.AccountName = strings.TrimSpace(*in.AccountName)
	}
	if in.Note != nil {
		next.Note = strings.TrimSpace(*in.Note)
	}
	if in.Digits != 0 {
		next.Digits = in.Digits
	}
	if in.Period != 0 {
		next.Period = in.Period
	}
	if in.Algorithm != "" {
		next.Algorithm = in.Algorithm
	}
	parsed, err := totp.Parse("AAAA", next)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_totp")
		return totp.Entry{}, "", false
	}
	parsed.Secret = ""
	return parsed, "", true
}

func (s *Server) validTotpInput(w http.ResponseWriter, in totpInput, creating bool) bool {
	if utf8.RuneCountInString(in.Input) > totpInputLimit || utf8.RuneCountInString(in.Secret) > totpInputLimit {
		writeError(w, http.StatusBadRequest, "invalid_totp")
		return false
	}
	for _, value := range []string{in.Name, textOf(in.Issuer), textOf(in.AccountName), textOf(in.Note)} {
		if utf8.RuneCountInString(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
			writeError(w, http.StatusBadRequest, "invalid_totp")
			return false
		}
	}
	if creating && strings.TrimSpace(in.Input) == "" && strings.TrimSpace(in.Secret) == "" {
		writeError(w, http.StatusBadRequest, "invalid_totp")
		return false
	}
	return true
}

func (s *Server) writeTotpStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, store.ErrTotpPrivate):
		writeError(w, http.StatusNotFound, "not_found")
	default:
		s.internalError(w, r, err)
	}
}

func entryFromParsed(p totp.Entry, owner, createdBy string) *store.TotpEntry {
	return &store.TotpEntry{
		OwnerUserID: owner, Name: p.Name, Issuer: p.Issuer, AccountName: p.AccountName,
		Digits: p.Digits, Period: p.Period, Algorithm: p.Algorithm, Note: p.Note, CreatedBy: createdBy,
	}
}

func uniqueIDs(ids []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
