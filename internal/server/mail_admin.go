package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"xauth/internal/store"
	"xauth/internal/xmail"
)

// This file holds the admin mail endpoints: a proxy that forwards the xmail
// REST endpoints listed in xmail.AdminRoutes, the mailbox grants that live in
// XAuth, and the sandboxed message view.

const mailProxyPrefix = "/api/admin/mail/"

// proxySegments splits the path after /api/admin/mail/ into decoded
// segments. Any empty, dot or separator-bearing segment fails.
func proxySegments(r *http.Request) ([]string, bool) {
	p := r.URL.EscapedPath()
	rest, ok := strings.CutPrefix(p, mailProxyPrefix)
	if !ok || rest == "" {
		return nil, false
	}
	raw := strings.Split(rest, "/")
	segs := make([]string, 0, len(raw))
	for _, s := range raw {
		v, err := url.PathUnescape(s)
		if err != nil || !xmail.ValidSegment(v) {
			return nil, false
		}
		segs = append(segs, v)
	}
	return segs, true
}

// adminMailError answers a failed xmail call made for an admin. Admins see
// xmail's own message; the API key is never part of it.
func (s *Server) adminMailError(w http.ResponseWriter, r *http.Request, res *xmail.Result, err error) {
	if err != nil {
		s.log.Warn("xmail call failed", "path", r.URL.Path, "err", err)
		switch {
		case errors.Is(err, xmail.ErrTooLarge):
			writeErrorMessage(w, http.StatusBadGateway, "xmail_error", "xmail 返回的内容过大。")
		case errors.Is(err, xmail.ErrBadResponse):
			writeErrorMessage(w, http.StatusBadGateway, "xmail_error", "xmail 返回了无法识别的响应。")
		default:
			writeError(w, http.StatusBadGateway, "xmail_unreachable")
		}
		return
	}
	switch res.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		writeError(w, http.StatusBadGateway, "xmail_unauthorized")
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict:
		writeErrorMessage(w, res.Status, "xmail_error", xmailMessage(res))
	default:
		s.log.Warn("xmail call failed", "path", r.URL.Path, "status", res.Status, "error", res.Error)
		writeErrorMessage(w, http.StatusBadGateway, "xmail_error", xmailMessage(res))
	}
}

func xmailMessage(res *xmail.Result) string {
	msg := strings.TrimSpace(res.Error)
	if msg == "" {
		return errorMessages["xmail_error"]
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return "xmail：" + msg
}

// handleMailProxy forwards one documented xmail endpoint. The browser only
// chooses the method, the path segments, the query and a JSON body; the
// host, the /api/v1 prefix and the API key are fixed here.
func (s *Server) handleMailProxy(w http.ResponseWriter, r *http.Request) {
	if !s.mailReady(w) {
		return
	}
	segs, ok := proxySegments(r)
	if !ok || !xmail.AdminAllowed(r.Method, segs) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if len(r.URL.RawQuery) > 4096 {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	var body []byte
	if r.Method != http.MethodGet {
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large")
			return
		}
		body = bytes.TrimSpace(body)
		if len(body) == 0 {
			body = nil
		} else if !json.Valid(body) {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
	}
	c := s.xmailClient()
	if c == nil {
		writeError(w, http.StatusServiceUnavailable, "xmail_not_configured")
		return
	}
	res, err := c.Do(r.Context(), r.Method, segs, query, body)
	if err != nil || !res.OK() {
		s.adminMailError(w, r, res, err)
		return
	}
	if r.Method == http.MethodGet {
		switch {
		case xmail.Match("GET mailboxes/*/emails", r.Method, segs), xmail.Match("GET mailboxes/*/verification-code", r.Method, segs):
			s.recordAdminView(r, segs[1], "")
		case xmail.Match("GET emails/*", r.Method, segs) && segs[1] != "starred":
			var data struct {
				Email struct {
					Mailbox string `json:"mailbox"`
				} `json:"email"`
			}
			if res.Decode(&data) == nil {
				s.recordAdminView(r, data.Email.Mailbox, segs[1])
			}
		}
	}
	data := res.Data
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(res.Status)
	_, _ = w.Write(data)
}

// handleAdminMailHTML serves a message body for the admin inbox.
func (s *Server) handleAdminMailHTML(w http.ResponseWriter, r *http.Request) {
	if !s.mailReady(w) {
		return
	}
	id, ok := pathParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	c := s.xmailClient()
	if c == nil {
		writeError(w, http.StatusServiceUnavailable, "xmail_not_configured")
		return
	}
	res, err := c.Get(r.Context(), []string{"emails", id}, nil)
	if err != nil || !res.OK() {
		s.adminMailError(w, r, res, err)
		return
	}
	var data struct {
		Email emailDetailJSON `json:"email"`
	}
	if err := res.Decode(&data); err != nil {
		s.adminMailError(w, r, res, xmail.ErrBadResponse)
		return
	}
	s.recordAdminView(r, data.Email.Mailbox, id)
	writeMailHTML(w, deref(data.Email.HTML), deref(data.Email.Text))
}

type mailGrantJSON struct {
	Address string   `json:"address"`
	UserIDs []string `json:"userIds"`
	// Status is set when the list was checked against xmail: "ok",
	// "trashed" (soft-deleted, restorable), "missing" or "unknown".
	Status string `json:"status,omitempty"`
}

func (s *Server) handleListMailGrants(w http.ResponseWriter, r *http.Request) {
	check := r.URL.Query().Get("check") == "1"
	if check && !s.mailReady(w) {
		return
	}
	rows, err := s.st.AllMailboxGrants(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	byAddr := map[string]*mailGrantJSON{}
	out := []*mailGrantJSON{}
	for _, g := range rows {
		m := byAddr[g.Address]
		if m == nil {
			m = &mailGrantJSON{Address: g.Address, UserIDs: []string{}}
			byAddr[g.Address] = m
			out = append(out, m)
		}
		m.UserIDs = append(m.UserIDs, g.UserID)
	}
	if check {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for _, m := range out {
			wg.Add(1)
			go func(m *mailGrantJSON) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				d, found, res, err := s.fetchMailbox(r.Context(), m.Address)
				switch {
				case err != nil || res == nil:
					m.Status = "unknown"
				case res.Status == http.StatusNotFound:
					m.Status = "missing"
				case !res.OK():
					m.Status = "unknown"
				case !found && d != nil:
					m.Status = "trashed"
				default:
					m.Status = "ok"
				}
			}(m)
		}
		wg.Wait()
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}

// handleSetMailGrants replaces who may read a mailbox. Adding users needs
// the mailbox to exist in xmail; removing never does.
func (s *Server) handleSetMailGrants(w http.ResponseWriter, r *http.Request) {
	me := sessionFrom(r.Context())
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_mailbox")
		return
	}
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
	current, err := s.st.MailboxUserIDs(r.Context(), addr)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	have := map[string]bool{}
	for _, id := range current {
		have[id] = true
	}
	adding := false
	for _, id := range req.UserIDs {
		adding = adding || !have[id]
	}
	if adding {
		if !s.mailReady(w) {
			return
		}
		_, found, res, err := s.fetchMailbox(r.Context(), addr)
		if err != nil || (res != nil && !res.OK() && res.Status != http.StatusNotFound) {
			s.adminMailError(w, r, res, err)
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "mailbox_not_found")
			return
		}
	}
	added, removed, err := s.st.SetMailboxUsers(r.Context(), addr, req.UserIDs, me.user.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "unknown_user")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.auditGrants(r, addr, added, store.EvMailAssign, nil)
	s.auditGrants(r, addr, removed, store.EvMailUnassign, nil)
	ids, err := s.st.MailboxUserIDs(r.Context(), addr)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mailGrantJSON{Address: addr, UserIDs: ids})
}

// handleDeleteMailGrants clears every grant of a mailbox, typically one that
// was deleted in xmail.
func (s *Server) handleDeleteMailGrants(w http.ResponseWriter, r *http.Request) {
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_mailbox")
		return
	}
	removed, err := s.st.DeleteMailboxGrants(r.Context(), addr)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.auditGrants(r, addr, removed, store.EvMailUnassign, map[string]any{"cleanup": true})
	writeJSON(w, http.StatusOK, map[string]any{"removed": len(removed)})
}

func (s *Server) auditGrants(r *http.Request, addr string, userIDs []string, typ string, extra map[string]any) {
	me := sessionFrom(r.Context())
	sort.Strings(userIDs)
	for _, id := range userIDs {
		detail := map[string]any{"mailbox": addr}
		for k, v := range extra {
			detail[k] = v
		}
		s.audit(r.Context(), store.Event{Type: typ, ActorID: me.user.ID, SubjectType: "user", SubjectID: id,
			IP: s.clientIP(r), Detail: detail})
	}
}
