package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"xauth/internal/outlook"
	"xauth/internal/store"
)

type latestCodeJSON struct {
	Code       string  `json:"code"`
	Subject    *string `json:"subject,omitempty"`
	From       string  `json:"from,omitempty"`
	FromName   *string `json:"fromName,omitempty"`
	ReceivedAt int64   `json:"receivedAt,omitempty"`
	EmailID    string  `json:"emailId,omitempty"`
}

func extractLatestCode(items []outlook.MessageItem) *latestCodeJSON {
	for _, it := range items {
		if it.VerificationCode != nil && *it.VerificationCode != "" {
			return &latestCodeJSON{
				Code:       *it.VerificationCode,
				Subject:    it.Subject,
				From:       it.From,
				FromName:   it.FromName,
				ReceivedAt: it.CreatedAt,
				EmailID:    it.ID,
			}
		}
	}
	return nil
}

func (s *Server) writeOutlookFetchError(w http.ResponseWriter, r *http.Request, err error, admin bool) {
	switch {
	case errors.Is(err, outlook.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, outlook.ErrAccountAbuse):
		writeError(w, http.StatusBadGateway, "outlook_abuse")
	case errors.Is(err, outlook.ErrInvalidGrant):
		writeError(w, http.StatusBadGateway, "outlook_invalid_grant")
	default:
		s.log.Warn("outlook fetch failed", "path", r.URL.Path, "err", err)
		if admin && err != nil {
			msg := err.Error()
			if len(msg) > 240 {
				msg = msg[:240]
			}
			writeErrorMessage(w, http.StatusBadGateway, "outlook_fetch_failed", "Outlook 取信失败："+msg)
			return
		}
		writeError(w, http.StatusBadGateway, "outlook_fetch_failed")
	}
}

func outlookQueryMethodAndTop(q url.Values) (method string, top int, ok bool) {
	method = strings.TrimSpace(q.Get("method"))
	if !outlook.ValidMethod(method) {
		return "", 0, false
	}
	if method == "" {
		method = outlook.MethodAuto
	}
	top = 20
	if v := strings.TrimSpace(q.Get("top")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			return "", 0, false
		}
		top = n
	}
	return method, top, true
}

// ==================== Regular User Endpoints (/api/outlook/*) ====================

func (s *Server) handleMyOutlookMailboxes(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	boxes, err := s.st.UserOutlookMailboxes(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mailboxes": boxes})
}

func (s *Server) grantedOutlookAccount(w http.ResponseWriter, r *http.Request, info *sessionInfo) (*store.OutlookAccount, bool) {
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	acc, err := s.st.UserGrantedOutlookAccount(r.Context(), info.user.ID, addr)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	if acc.Status != "active" {
		writeError(w, http.StatusForbidden, "mailbox_disabled")
		return nil, false
	}
	return acc, true
}

func (s *Server) handleMyOutlookEmails(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	method, top, ok := outlookQueryMethodAndTop(r.URL.Query())
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	acc, ok := s.grantedOutlookAccount(w, r, info)
	if !ok {
		return
	}
	creds := outlook.Credentials{
		Email:        acc.Email,
		ClientID:     acc.ClientID,
		RefreshToken: acc.RefreshToken,
	}
	items, usedMethod, err := s.outlookClient().ListMessages(r.Context(), creds, method, top)
	if err != nil {
		s.writeOutlookFetchError(w, r, err, false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"method":     usedMethod,
		"latestCode": extractLatestCode(items),
	})
}

func (s *Server) handleMyOutlookEmail(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	msgID, ok := pathParam(r, "messageID")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	method := strings.TrimSpace(r.URL.Query().Get("method"))
	if !outlook.ValidMethod(method) {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	acc, ok := s.grantedOutlookAccount(w, r, info)
	if !ok {
		return
	}
	creds := outlook.Credentials{
		Email:        acc.Email,
		ClientID:     acc.ClientID,
		RefreshToken: acc.RefreshToken,
	}
	detail, err := s.outlookClient().GetMessage(r.Context(), creds, msgID, method)
	if err != nil {
		s.writeOutlookFetchError(w, r, err, false)
		return
	}
	out := *detail
	out.HTML = nil
	writeJSON(w, http.StatusOK, map[string]any{"email": out})
}

func (s *Server) handleMyOutlookEmailHTML(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	msgID, ok := pathParam(r, "messageID")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	method := strings.TrimSpace(r.URL.Query().Get("method"))
	if !outlook.ValidMethod(method) {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	acc, ok := s.grantedOutlookAccount(w, r, info)
	if !ok {
		return
	}
	creds := outlook.Credentials{
		Email:        acc.Email,
		ClientID:     acc.ClientID,
		RefreshToken: acc.RefreshToken,
	}
	detail, err := s.outlookClient().GetMessage(r.Context(), creds, msgID, method)
	if err != nil {
		s.writeOutlookFetchError(w, r, err, false)
		return
	}
	writeMailHTML(w, deref(detail.HTML), deref(detail.Text))
}

// ==================== Admin Endpoints (/api/admin/outlook/*) ====================

func (s *Server) handleListOutlookGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.st.ListOutlookGroups(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

type outlookGroupInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

func validOutlookGroupInput(w http.ResponseWriter, in outlookGroupInput) bool {
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 || strings.ContainsAny(name, "\r\n\x00") {
		writeError(w, http.StatusBadRequest, "invalid_name")
		return false
	}
	if utf8.RuneCountInString(in.Description) > 200 || strings.ContainsAny(in.Description, "\r\n\x00") {
		writeError(w, http.StatusBadRequest, "invalid_note")
		return false
	}
	return true
}

func (s *Server) handleCreateOutlookGroup(w http.ResponseWriter, r *http.Request) {
	var in outlookGroupInput
	if !decode(w, r, &in) || !validOutlookGroupInput(w, in) {
		return
	}
	g, err := s.st.CreateOutlookGroup(r.Context(), in.Name, in.Description, in.Color)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "outlook_group_exists")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": g})
}

func (s *Server) handleUpdateOutlookGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var in outlookGroupInput
	if !decode(w, r, &in) || !validOutlookGroupInput(w, in) {
		return
	}
	g, err := s.st.UpdateOutlookGroup(r.Context(), id, in.Name, in.Description, in.Color)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "outlook_group_exists")
	case err != nil:
		s.internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"group": g})
	}
}

func (s *Server) handleDeleteOutlookGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	err = s.st.DeleteOutlookGroup(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrDefaultGroup):
		writeError(w, http.StatusBadRequest, "outlook_default_group")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case err != nil:
		s.internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (s *Server) handleListOutlookAccounts(w http.ResponseWriter, r *http.Request) {
	var groupID int64
	if v := strings.TrimSpace(r.URL.Query().Get("group_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 0 {
			writeError(w, http.StatusBadRequest, "invalid_query")
			return
		}
		groupID = id
	}
	accounts, err := s.st.ListOutlookAccounts(r.Context(), groupID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

func (s *Server) handleGetOutlookAccount(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	acc, err := s.st.OutlookAccountByID(r.Context(), id, true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": acc})
}

func (s *Server) handleImportOutlookAccounts(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	var req struct {
		AccountsText string   `json:"accountsText"`
		GroupID      int64    `json:"groupId"`
		Remark       string   `json:"remark"`
		UserIDs      []string `json:"userIds"`
		Overwrite    bool     `json:"overwrite"`
	}
	if !decodeLimit(w, r, 2<<20, &req) {
		return
	}
	if utf8.RuneCountInString(req.Remark) > 200 || strings.ContainsAny(req.Remark, "\r\n\x00") {
		writeError(w, http.StatusBadRequest, "invalid_note")
		return
	}
	if len(req.UserIDs) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, parseErrs := outlook.ParseAccountsText(req.AccountsText)
	if len(parsed) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":       "outlook_empty_import",
			"message":     errorMessages["outlook_empty_import"],
			"parseErrors": parseErrs,
		})
		return
	}

	added, updated, skipped, err := s.st.ImportOutlookAccounts(
		r.Context(),
		parsed,
		req.GroupID,
		req.Remark,
		req.UserIDs,
		req.Overwrite,
		me.user.ID,
	)
	if errors.Is(err, store.ErrUnknownUser) || errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "unknown_user")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	s.audit(r.Context(), store.Event{
		Type:        store.EvOutlookImport,
		ActorID:     me.user.ID,
		SubjectType: "mailbox",
		SubjectName: fmt.Sprintf("批量导入 %d 个 Outlook 邮箱", added+updated),
		IP:          s.clientIP(r),
		Detail: map[string]any{
			"added":   added,
			"updated": updated,
			"skipped": skipped,
			"users":   len(uniqueIDs(req.UserIDs)),
		},
	})
	if len(req.UserIDs) > 0 && len(parsed) <= 50 {
		for _, item := range parsed {
			s.auditGrants(r, item.Email, uniqueIDs(req.UserIDs), store.EvMailAssign, map[string]any{"provider": "outlook"})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"added":       added,
		"updated":     updated,
		"skipped":     skipped,
		"parseErrors": parseErrs,
	})
}

func (s *Server) handleUpdateOutlookAccount(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var req struct {
		Email        string   `json:"email"`
		Password     *string  `json:"password"`
		ClientID     string   `json:"clientId"`
		RefreshToken string   `json:"refreshToken"`
		GroupID      int64    `json:"groupId"`
		Remark       *string  `json:"remark"`
		Status       string   `json:"status"`
		UserIDs      []string `json:"userIds"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Email != "" && !outlook.ValidEmail(outlook.NormalizeEmail(req.Email)) {
		writeError(w, http.StatusBadRequest, "invalid_mailbox")
		return
	}
	if strings.ContainsAny(strings.TrimSpace(req.ClientID), " \t\r\n\x00") ||
		strings.ContainsAny(strings.TrimSpace(req.RefreshToken), " \t\r\n\x00") {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.Remark != nil && (utf8.RuneCountInString(*req.Remark) > 200 || strings.ContainsAny(*req.Remark, "\r\n\x00")) {
		writeError(w, http.StatusBadRequest, "invalid_note")
		return
	}
	if req.Status != "" && req.Status != "active" && req.Status != "disabled" {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if len(req.UserIDs) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	acc, err := s.st.UpdateOutlookAccount(r.Context(), id, store.OutlookAccountUpdate{
		Email:        req.Email,
		Password:     req.Password,
		ClientID:     req.ClientID,
		RefreshToken: req.RefreshToken,
		GroupID:      req.GroupID,
		Remark:       req.Remark,
		Status:       req.Status,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "outlook_account_exists")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}

	if req.UserIDs != nil {
		added, removed, email, err := s.st.SetOutlookGrants(r.Context(), id, req.UserIDs, me.user.ID)
		if errors.Is(err, store.ErrUnknownUser) || errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "unknown_user")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		s.auditGrants(r, email, added, store.EvMailAssign, map[string]any{"provider": "outlook"})
		s.auditGrants(r, email, removed, store.EvMailUnassign, map[string]any{"provider": "outlook"})
		acc.UserIDs = uniqueIDs(req.UserIDs)
	}

	s.audit(r.Context(), store.Event{
		Type:        store.EvOutlookUpdate,
		ActorID:     me.user.ID,
		SubjectType: "mailbox",
		SubjectID:   acc.ID,
		SubjectName: acc.Email,
		IP:          s.clientIP(r),
		Detail: map[string]any{
			"status": acc.Status,
			"users":  len(acc.UserIDs),
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{"account": acc})
}

func (s *Server) handleDeleteOutlookAccount(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	emails, err := s.st.DeleteOutlookAccounts(r.Context(), []string{id})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if len(emails) == 0 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	s.audit(r.Context(), store.Event{
		Type:        store.EvOutlookDelete,
		ActorID:     me.user.ID,
		SubjectType: "mailbox",
		SubjectID:   id,
		SubjectName: emails[0],
		IP:          s.clientIP(r),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleBatchDeleteOutlookAccounts(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	emails, err := s.st.DeleteOutlookAccounts(r.Context(), req.IDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if len(emails) > 0 {
		s.audit(r.Context(), store.Event{
			Type:        store.EvOutlookDelete,
			ActorID:     me.user.ID,
			SubjectType: "mailbox",
			SubjectName: fmt.Sprintf("批量删除 %d 个 Outlook 邮箱", len(emails)),
			IP:          s.clientIP(r),
			Detail:      map[string]any{"count": len(emails)},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": len(emails)})
}

func (s *Server) handleSetOutlookGrants(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var req struct {
		UserIDs []string `json:"userIds"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.UserIDs) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	added, removed, email, err := s.st.SetOutlookGrants(r.Context(), id, req.UserIDs, me.user.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, store.ErrUnknownUser):
		writeError(w, http.StatusBadRequest, "unknown_user")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.auditGrants(r, email, added, store.EvMailAssign, map[string]any{"provider": "outlook"})
	s.auditGrants(r, email, removed, store.EvMailUnassign, map[string]any{"provider": "outlook"})
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"email":   email,
		"userIds": uniqueIDs(req.UserIDs),
	})
}

func (s *Server) handleBatchSetOutlookGrants(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	var req struct {
		AccountIDs []string `json:"accountIds"`
		UserIDs    []string `json:"userIds"`
		Mode       string   `json:"mode"` // "replace" or "append"
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.AccountIDs) > 1000 || len(req.UserIDs) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.Mode == "" {
		req.Mode = "replace"
	}
	if req.Mode != "replace" && req.Mode != "append" {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	changes, err := s.st.BatchSetOutlookGrants(r.Context(), req.AccountIDs, req.UserIDs, req.Mode, me.user.ID)
	if errors.Is(err, store.ErrUnknownUser) || errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "unknown_user")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if len(changes) <= 50 {
		for _, ch := range changes {
			s.auditGrants(r, ch.Email, ch.Added, store.EvMailAssign, map[string]any{"provider": "outlook", "batch": true})
			s.auditGrants(r, ch.Email, ch.Removed, store.EvMailUnassign, map[string]any{"provider": "outlook", "batch": true})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": len(changes)})
}

func (s *Server) handleBatchMoveOutlookGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountIDs []string `json:"accountIds"`
		GroupID    int64    `json:"groupId"`
	}
	if !decode(w, r, &req) || req.GroupID < 1 {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	n, err := s.st.BatchMoveOutlookGroup(r.Context(), req.AccountIDs, req.GroupID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": n})
}

func (s *Server) handleExportOutlookAccounts(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	q := r.URL.Query()
	var groupIDs []int64
	if rawGroups := strings.TrimSpace(q.Get("group_ids")); rawGroups != "" {
		for _, part := range strings.Split(rawGroups, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil || id < 1 {
				writeError(w, http.StatusBadRequest, "invalid_query")
				return
			}
			groupIDs = append(groupIDs, id)
		}
	}
	var accountIDs []string
	if rawIDs := strings.TrimSpace(q.Get("ids")); rawIDs != "" {
		accountIDs = strings.Split(rawIDs, ",")
	}

	accounts, err := s.st.ExportOutlookAccounts(r.Context(), groupIDs, accountIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	lines := make([]string, 0, len(accounts))
	for _, acc := range accounts {
		lines = append(lines, outlook.FormatAccountLine(acc.Email, acc.Password, acc.ClientID, acc.RefreshToken))
	}
	content := strings.Join(lines, "\n")
	filename := fmt.Sprintf("outlook_accounts_%s.txt", time.Now().Format("20060102_150405"))

	if len(accounts) > 0 {
		s.audit(r.Context(), store.Event{
			Type:        store.EvOutlookExport,
			ActorID:     me.user.ID,
			SubjectType: "mailbox",
			SubjectName: fmt.Sprintf("导出 %d 个 Outlook 邮箱", len(accounts)),
			IP:          s.clientIP(r),
			Detail:      map[string]any{"count": len(accounts)},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content":  content,
		"count":    len(accounts),
		"filename": filename,
	})
}

func (s *Server) adminOutlookAccount(w http.ResponseWriter, r *http.Request) (*store.OutlookAccount, bool) {
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	acc, err := s.st.OutlookAccountByEmail(r.Context(), addr, true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return acc, true
}

func (s *Server) handleAdminOutlookEmails(w http.ResponseWriter, r *http.Request) {
	method, top, ok := outlookQueryMethodAndTop(r.URL.Query())
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	acc, ok := s.adminOutlookAccount(w, r)
	if !ok {
		return
	}
	creds := outlook.Credentials{
		Email:        acc.Email,
		ClientID:     acc.ClientID,
		RefreshToken: acc.RefreshToken,
	}
	items, usedMethod, err := s.outlookClient().ListMessages(r.Context(), creds, method, top)
	if err != nil {
		s.writeOutlookFetchError(w, r, err, true)
		return
	}
	s.recordAdminView(r, acc.Email, "")
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"method":     usedMethod,
		"latestCode": extractLatestCode(items),
	})
}

func (s *Server) handleAdminOutlookEmail(w http.ResponseWriter, r *http.Request) {
	msgID, ok := pathParam(r, "messageID")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	method := strings.TrimSpace(r.URL.Query().Get("method"))
	if !outlook.ValidMethod(method) {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	acc, ok := s.adminOutlookAccount(w, r)
	if !ok {
		return
	}
	creds := outlook.Credentials{
		Email:        acc.Email,
		ClientID:     acc.ClientID,
		RefreshToken: acc.RefreshToken,
	}
	detail, err := s.outlookClient().GetMessage(r.Context(), creds, msgID, method)
	if err != nil {
		s.writeOutlookFetchError(w, r, err, true)
		return
	}
	s.recordAdminView(r, acc.Email, msgID)
	out := *detail
	out.HTML = nil
	writeJSON(w, http.StatusOK, map[string]any{"email": out})
}

func (s *Server) handleAdminOutlookEmailHTML(w http.ResponseWriter, r *http.Request) {
	msgID, ok := pathParam(r, "messageID")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	method := strings.TrimSpace(r.URL.Query().Get("method"))
	if !outlook.ValidMethod(method) {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	acc, ok := s.adminOutlookAccount(w, r)
	if !ok {
		return
	}
	creds := outlook.Credentials{
		Email:        acc.Email,
		ClientID:     acc.ClientID,
		RefreshToken: acc.RefreshToken,
	}
	detail, err := s.outlookClient().GetMessage(r.Context(), creds, msgID, method)
	if err != nil {
		s.writeOutlookFetchError(w, r, err, true)
		return
	}
	s.recordAdminView(r, acc.Email, msgID)
	writeMailHTML(w, deref(detail.HTML), deref(detail.Text))
}
