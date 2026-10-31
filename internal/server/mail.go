package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"xauth/internal/store"
	"xauth/internal/xmail"
)

// This file holds the mail endpoints for regular users. They only ever see
// mailboxes granted to them in XAuth; a mailbox that is not granted answers
// 404 exactly like one that does not exist.

// mailReady writes 503 when xmail is not configured.
func (s *Server) mailReady(w http.ResponseWriter) bool {
	if s.mail == nil {
		writeError(w, http.StatusServiceUnavailable, "xmail_not_configured")
		return false
	}
	return true
}

// pathParam returns a percent-decoded route parameter that is safe to use
// as one xmail path segment.
func pathParam(r *http.Request, name string) (string, bool) {
	v, err := url.PathUnescape(chi.URLParam(r, name))
	return v, err == nil && xmail.ValidSegment(v)
}

// validMailbox checks the shape of a mailbox address (already normalised).
func validMailbox(addr string) bool {
	local, domain, ok := strings.Cut(addr, "@")
	return ok && local != "" && domain != "" && !strings.Contains(domain, "@") &&
		len(addr) <= 254 && xmail.ValidSegment(addr) && !strings.ContainsAny(addr, " \t")
}

func mailboxParam(r *http.Request) (string, bool) {
	v, ok := pathParam(r, "address")
	if !ok {
		return "", false
	}
	v = store.NormalizeMailbox(v)
	return v, validMailbox(v)
}

// userMailError answers a failed xmail call made on behalf of a regular
// user. xmail's own message is logged, never shown.
func (s *Server) userMailError(w http.ResponseWriter, r *http.Request, res *xmail.Result, err error) {
	if err == nil && res.Status == http.StatusNotFound {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.log.Warn("xmail call failed", "path", r.URL.Path, "err", err)
	} else {
		s.log.Warn("xmail call failed", "path", r.URL.Path, "status", res.Status, "error", res.Error)
	}
	writeError(w, http.StatusBadGateway, "mail_unavailable")
}

// xmMailbox is the part of xmail's mailbox object XAuth reads.
type xmMailbox struct {
	Address   string  `json:"address"`
	Note      *string `json:"note"`
	ExpiresAt *int64  `json:"expiresAt"`
	IsActive  bool    `json:"isActive"`
	DeletedAt *int64  `json:"deletedAt"`
}

type xmService struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	LoginURL  string  `json:"loginUrl"`
	Note      *string `json:"note"`
	ExpiresAt *int64  `json:"expiresAt"`
}

type xmStats struct {
	Total  int `json:"total"`
	Unread int `json:"unread"`
}

type xmDetail struct {
	Mailbox  xmMailbox   `json:"mailbox"`
	Stats    xmStats     `json:"stats"`
	Services []xmService `json:"services"`
}

// fetchMailbox loads a mailbox from xmail. found is false when xmail does
// not have it or it is in the trash.
func (s *Server) fetchMailbox(ctx context.Context, addr string) (d *xmDetail, found bool, res *xmail.Result, err error) {
	res, err = s.mail.Get(ctx, []string{"mailboxes", addr}, nil)
	if err != nil {
		return nil, false, nil, err
	}
	if res.Status == http.StatusNotFound {
		return nil, false, res, nil
	}
	if !res.OK() {
		return nil, false, res, nil
	}
	d = &xmDetail{}
	if err := res.Decode(d); err != nil {
		return nil, false, res, fmt.Errorf("decode mailbox: %w", err)
	}
	return d, d.Mailbox.DeletedAt == nil, res, nil
}

// grantedMailbox checks that the signed-in user holds a grant for addr and
// that the mailbox is usable in xmail. It writes the error response itself.
func (s *Server) grantedMailbox(w http.ResponseWriter, r *http.Request, info *sessionInfo, addr string) (*xmDetail, bool) {
	ok, err := s.st.UserHasMailbox(r.Context(), info.user.ID, addr)
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	d, found, res, err := s.fetchMailbox(r.Context(), addr)
	if err != nil || (res != nil && !res.OK() && res.Status != http.StatusNotFound) {
		s.userMailError(w, r, res, err)
		return nil, false
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if !d.Mailbox.IsActive {
		writeError(w, http.StatusForbidden, "mailbox_disabled")
		return nil, false
	}
	if d.Mailbox.ExpiresAt != nil && *d.Mailbox.ExpiresAt > 0 && *d.Mailbox.ExpiresAt < s.st.Now().Unix() {
		writeError(w, http.StatusForbidden, "mailbox_expired")
		return nil, false
	}
	return d, true
}

func (s *Server) handleMailStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": s.mail != nil})
}

// requireMailUser is the guard for every user mail route: signed in, then
// xmail configured.
func (s *Server) requireMailUser(w http.ResponseWriter, r *http.Request) (*sessionInfo, bool) {
	info, ok := s.requireUser(w, r)
	if !ok || !s.mailReady(w) {
		return nil, false
	}
	return info, true
}

type myMailboxJSON struct {
	Address    string    `json:"address"`
	AssignedAt time.Time `json:"assignedAt"`
}

func (s *Server) handleMyMailboxes(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	grants, err := s.st.UserMailboxes(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]myMailboxJSON, 0, len(grants))
	for _, g := range grants {
		out = append(out, myMailboxJSON{Address: g.Address, AssignedAt: g.AssignedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"mailboxes": out})
}

type userServiceJSON struct {
	Name      string  `json:"name"`
	LoginURL  string  `json:"loginUrl"`
	Note      *string `json:"note"`
	ExpiresAt *int64  `json:"expiresAt"`
}

func (s *Server) handleMyMailbox(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	d, ok := s.grantedMailbox(w, r, info, addr)
	if !ok {
		return
	}
	services := make([]userServiceJSON, 0, len(d.Services))
	for _, sv := range d.Services {
		services = append(services, userServiceJSON{Name: sv.Name, LoginURL: sv.LoginURL, Note: sv.Note, ExpiresAt: sv.ExpiresAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mailbox":  map[string]any{"address": addr, "expiresAt": d.Mailbox.ExpiresAt, "isActive": d.Mailbox.IsActive},
		"stats":    d.Stats,
		"services": services,
	})
}

func (s *Server) handleMyMailboxStats(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	d, ok := s.grantedMailbox(w, r, info, addr)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"address": addr,
		"total":   d.Stats.Total,
		"unread":  d.Stats.Unread,
	})
}

func (s *Server) handleMyMailboxServices(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	d, ok := s.grantedMailbox(w, r, info, addr)
	if !ok {
		return
	}
	services := make([]userServiceJSON, 0, len(d.Services))
	for _, sv := range d.Services {
		services = append(services, userServiceJSON{Name: sv.Name, LoginURL: sv.LoginURL, Note: sv.Note, ExpiresAt: sv.ExpiresAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"address": addr,
		"items":   services,
	})
}

// emailJSON is the list form of a message shown to users.
type emailJSON struct {
	ID               string  `json:"id"`
	Mailbox          string  `json:"mailbox"`
	From             string  `json:"from"`
	FromName         *string `json:"fromName"`
	To               string  `json:"to"`
	Subject          *string `json:"subject"`
	IsRead           bool    `json:"isRead"`
	IsStarred        bool    `json:"isStarred"`
	CreatedAt        int64   `json:"createdAt"`
	Preview          string  `json:"preview"`
	VerificationCode *string `json:"verificationCode"`
}

// emailDetailJSON adds the plain-text body. HTML is served separately by
// the sandboxed /html endpoint and never handed to the web app as markup.
type emailDetailJSON struct {
	emailJSON
	Text    *string `json:"text"`
	HTML    *string `json:"html,omitempty"`
	HasHTML bool    `json:"hasHtml"`
}

// userEmailQuery copies the list filters a user may set.
func userEmailQuery(q url.Values) (url.Values, bool) {
	out := url.Values{}
	for _, k := range []string{"q", "from", "subject"} {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			if len(v) > 200 {
				return nil, false
			}
			out.Set(k, v)
		}
	}
	for _, k := range []string{"is_read", "is_starred"} {
		switch v := q.Get(k); v {
		case "":
		case "true", "false":
			out.Set(k, v)
		default:
			return nil, false
		}
	}
	limit, offset := 50, 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, false
		}
		limit = min(n, 100)
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 100000 {
			return nil, false
		}
		offset = n
	}
	out.Set("limit", strconv.Itoa(limit))
	out.Set("offset", strconv.Itoa(offset))
	return out, true
}

func (s *Server) handleMyEmails(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	query, ok := userEmailQuery(r.URL.Query())
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	if _, ok := s.grantedMailbox(w, r, info, addr); !ok {
		return
	}
	res, err := s.mail.Get(r.Context(), []string{"mailboxes", addr, "emails"}, query)
	if err != nil || !res.OK() {
		s.userMailError(w, r, res, err)
		return
	}
	var data struct {
		Items []emailJSON `json:"items"`
		Total *int        `json:"total"`
	}
	if err := res.Decode(&data); err != nil {
		s.userMailError(w, r, res, err)
		return
	}
	if data.Items == nil {
		data.Items = []emailJSON{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": data.Items, "total": data.Total})
}

func (s *Server) handleMyCode(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	addr, ok := mailboxParam(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	seconds := 600
	if v := r.URL.Query().Get("seconds"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 86400 {
			writeError(w, http.StatusBadRequest, "invalid_query")
			return
		}
		seconds = n
	}
	if _, ok := s.grantedMailbox(w, r, info, addr); !ok {
		return
	}
	res, err := s.mail.Get(r.Context(), []string{"mailboxes", addr, "verification-code"},
		url.Values{"seconds": {strconv.Itoa(seconds)}})
	if err != nil || !res.OK() {
		s.userMailError(w, r, res, err)
		return
	}
	var code struct {
		Code       *string `json:"code"`
		Subject    *string `json:"subject"`
		From       *string `json:"from"`
		FromName   *string `json:"fromName"`
		ReceivedAt *int64  `json:"receivedAt"`
		EmailID    *string `json:"emailId"`
	}
	if err := res.Decode(&code); err != nil {
		s.userMailError(w, r, res, err)
		return
	}
	if code.Code == nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": nil})
		return
	}
	writeJSON(w, http.StatusOK, code)
}

// grantedEmail loads a message and checks that its mailbox is granted to
// the user. Anything else is 404.
func (s *Server) grantedEmail(w http.ResponseWriter, r *http.Request, info *sessionInfo) (*emailDetailJSON, bool) {
	id, ok := pathParam(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	res, err := s.mail.Get(r.Context(), []string{"emails", id}, nil)
	if err != nil || !res.OK() {
		s.userMailError(w, r, res, err)
		return nil, false
	}
	var data struct {
		Email emailDetailJSON `json:"email"`
	}
	if err := res.Decode(&data); err != nil {
		s.userMailError(w, r, res, err)
		return nil, false
	}
	addr := store.NormalizeMailbox(data.Email.Mailbox)
	if !validMailbox(addr) {
		writeError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	if _, ok := s.grantedMailbox(w, r, info, addr); !ok {
		return nil, false
	}
	data.Email.HasHTML = data.Email.HTML != nil && *data.Email.HTML != ""
	return &data.Email, true
}

func (s *Server) handleMyEmail(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	e, ok := s.grantedEmail(w, r, info)
	if !ok {
		return
	}
	e.HTML = nil
	writeJSON(w, http.StatusOK, map[string]any{"email": e})
}

func (s *Server) handleMyEmailHTML(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	e, ok := s.grantedEmail(w, r, info)
	if !ok {
		return
	}
	writeMailHTML(w, deref(e.HTML), deref(e.Text))
}

func (s *Server) handleMyEmailRead(w http.ResponseWriter, r *http.Request) {
	s.updateMyEmail(w, r, "read", nil)
}

func (s *Server) handleMyEmailStar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IsStarred *bool `json:"isStarred"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.IsStarred == nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	body, _ := json.Marshal(map[string]bool{"isStarred": *req.IsStarred})
	s.updateMyEmail(w, r, "star", body)
}

func (s *Server) updateMyEmail(w http.ResponseWriter, r *http.Request, action string, body []byte) {
	info, ok := s.requireMailUser(w, r)
	if !ok {
		return
	}
	e, ok := s.grantedEmail(w, r, info)
	if !ok {
		return
	}
	if body == nil {
		body = []byte("{}")
	}
	res, err := s.mail.Do(r.Context(), http.MethodPost, []string{"emails", e.ID, action}, nil, body)
	if err != nil || !res.OK() {
		s.userMailError(w, r, res, err)
		return
	}
	var data struct {
		Email emailJSON `json:"email"`
	}
	if err := res.Decode(&data); err != nil {
		s.userMailError(w, r, res, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": data.Email})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// mailCSP renders untrusted mail HTML in an opaque origin with no scripts,
// no remote loads (tracking pixels stay blocked) and no forms. Links open in
// a new tab outside the sandbox.
const mailCSP = "sandbox allow-popups allow-popups-to-escape-sandbox; default-src 'none'; " +
	"style-src 'unsafe-inline'; img-src data:; font-src data:; form-action 'none'; frame-ancestors 'self'"

const mailHead = `<!doctype html><html><head><meta charset="utf-8"><base target="_blank">` +
	`<style>body{margin:16px;font:14px/1.6 system-ui,sans-serif;color:#1b2340;overflow-wrap:anywhere}` +
	`img{max-width:100%;height:auto}pre{white-space:pre-wrap;font:inherit}</style></head><body>`

// writeMailHTML serves a message body for a sandboxed iframe in the web app.
func writeMailHTML(w http.ResponseWriter, html, text string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", mailCSP)
	h.Set("X-Frame-Options", "SAMEORIGIN")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(mailHead))
	if html != "" {
		_, _ = w.Write([]byte(html))
	} else {
		_, _ = w.Write([]byte("<pre>" + template.HTMLEscapeString(text) + "</pre>"))
	}
	_, _ = w.Write([]byte("</body></html>"))
}

// viewLog remembers recent admin views so browsing one inbox records one
// mail.admin_view event per 10 minutes instead of one per refresh.
type viewLog struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

const viewWindow = 10 * time.Minute

func (v *viewLog) fresh(key string, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.seen == nil {
		v.seen = map[string]time.Time{}
	}
	if t, ok := v.seen[key]; ok && now.Sub(t) < viewWindow {
		return false
	}
	if len(v.seen) > 2000 {
		for k, t := range v.seen {
			if now.Sub(t) >= viewWindow {
				delete(v.seen, k)
			}
		}
	}
	v.seen[key] = now
	return true
}

// recordAdminView writes mail.admin_view when an admin reads a mailbox or a
// message through the admin pages. The message body is never recorded.
func (s *Server) recordAdminView(r *http.Request, addr, emailID string) {
	me := sessionFrom(r.Context())
	addr = store.NormalizeMailbox(addr)
	if me == nil || addr == "" || !s.views.fresh(me.user.ID+"|"+addr+"|"+emailID, s.st.Now()) {
		return
	}
	detail := map[string]any{}
	if emailID != "" {
		detail["emailId"] = emailID
	}
	s.audit(r.Context(), store.Event{Type: store.EvMailAdminView, ActorID: me.user.ID, SubjectType: "mailbox",
		SubjectID: addr, SubjectName: addr, IP: s.clientIP(r), Detail: detail})
}
