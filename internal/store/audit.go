package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Audit event types.
const (
	EvLoginSuccess         = "login.success"
	EvLoginFailure         = "login.failure"
	EvRegister             = "user.register"
	EvLogout               = "logout"
	EvPasswordChange       = "password.change"
	EvSessionRevoke        = "session.revoke"
	EvAppSignIn            = "app.signin"
	EvAppDenied            = "app.denied"
	EvGrantRevoke          = "grant.revoke"
	EvTokenReuse           = "token.reuse"
	EvUserCreate           = "admin.user_create"
	EvUserUpdate           = "admin.user_update"
	EvUserSignout          = "admin.user_signout"
	EvAppCreate            = "admin.app_create"
	EvAppUpdate            = "admin.app_update"
	EvAppDelete            = "admin.app_delete"
	EvAppSecret            = "admin.app_secret"
	EvKeyRotate            = "admin.key_rotate"
	EvKeyRetire            = "admin.key_retire"
	EvMailAssign           = "mail.assign"
	EvMailUnassign         = "mail.unassign"
	EvMailAdminView        = "mail.admin_view"
	EvTotpCreate           = "totp.create"
	EvTotpUpdate           = "totp.update"
	EvTotpDelete           = "totp.delete"
	EvTotpAssign           = "totp.assign"
	EvEmailVerify          = "email.verify"
	EvPasswordResetRequest = "password.reset_request"
	EvPasswordReset        = "password.reset"
	EvPasskeyAdd           = "passkey.add"
	EvPasskeyRemove        = "passkey.remove"
	EvAvatarSet            = "avatar.set"
	EvAvatarRemove         = "avatar.remove"
)

// EventGroups are the filters offered in the admin log.
var EventGroups = map[string][]string{
	"signin":   {EvLoginSuccess, EvLoginFailure, EvRegister, EvLogout, EvPasswordChange, EvSessionRevoke, EvEmailVerify, EvPasswordResetRequest, EvPasswordReset, EvPasskeyAdd, EvPasskeyRemove, EvAvatarSet, EvAvatarRemove},
	"apps":     {EvAppSignIn, EvAppDenied, EvGrantRevoke},
	"admin":    {EvUserCreate, EvUserUpdate, EvUserSignout, EvAppCreate, EvAppUpdate, EvAppDelete, EvAppSecret, EvKeyRotate, EvKeyRetire, EvMailAssign, EvMailUnassign, EvTotpCreate, EvTotpUpdate, EvTotpDelete, EvTotpAssign},
	"security": {EvTokenReuse, EvLoginFailure, EvAppDenied},
	"mail":     {EvMailAssign, EvMailUnassign, EvMailAdminView},
}

// Event is one audit log entry. Names are snapshots taken when the event
// happened, so the log stays readable after a user or app is deleted.
type Event struct {
	ID          int64
	At          time.Time
	Type        string
	ActorID     string
	ActorEmail  string
	SubjectType string // "user", "app", "session", "key", "mailbox" or "totp"
	SubjectID   string
	SubjectName string
	IP          string
	Detail      map[string]any
}

type ipKey struct{}

// WithClientIP attaches the caller's address to a context so events recorded
// deep in the OIDC code still carry it.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ipKey{}, ip)
}

// Record writes an event. Missing actor emails and subject names are looked
// up from the current rows; a missing IP comes from the context.
func (s *Store) Record(ctx context.Context, e Event) error {
	if e.IP == "" {
		e.IP, _ = ctx.Value(ipKey{}).(string)
	}
	if e.ActorID != "" && e.ActorEmail == "" {
		_ = s.db.QueryRowContext(ctx, "SELECT email FROM users WHERE id = ?", e.ActorID).Scan(&e.ActorEmail)
	}
	if e.SubjectID != "" && e.SubjectName == "" {
		switch e.SubjectType {
		case "app":
			_ = s.db.QueryRowContext(ctx, "SELECT name FROM clients WHERE id = ?", e.SubjectID).Scan(&e.SubjectName)
		case "user":
			_ = s.db.QueryRowContext(ctx, "SELECT email FROM users WHERE id = ?", e.SubjectID).Scan(&e.SubjectName)
		}
	}
	detail := []byte("{}")
	if len(e.Detail) > 0 {
		var err error
		if detail, err = json.Marshal(e.Detail); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events (at, type, actor_id, actor_email, subject_type, subject_id, subject_name, ip, detail)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.unix(), e.Type, e.ActorID, truncate(e.ActorEmail, 254), e.SubjectType, e.SubjectID,
		truncate(e.SubjectName, 128), truncate(e.IP, 64), string(detail))
	return err
}

// EventFilter narrows ListEvents. Zero values match everything.
type EventFilter struct {
	Types    []string
	UserID   string // events where the user is the actor or the subject
	BeforeID int64  // keyset pagination: only events older than this ID
	Limit    int
}

// ListEvents returns matching events, newest first.
func (s *Store) ListEvents(ctx context.Context, f EventFilter) ([]Event, error) {
	q := "SELECT id, at, type, actor_id, actor_email, subject_type, subject_id, subject_name, ip, detail FROM audit_events WHERE 1 = 1"
	var args []any
	if len(f.Types) > 0 {
		q += " AND type IN (?" + strings.Repeat(", ?", len(f.Types)-1) + ")"
		for _, t := range f.Types {
			args = append(args, t)
		}
	}
	if f.UserID != "" {
		q += " AND (actor_id = ? OR (subject_type = 'user' AND subject_id = ?))"
		args = append(args, f.UserID, f.UserID)
	}
	if f.BeforeID > 0 {
		q += " AND id < ?"
		args = append(args, f.BeforeID)
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, f.Limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var at int64
		var detail string
		if err := rows.Scan(&e.ID, &at, &e.Type, &e.ActorID, &e.ActorEmail, &e.SubjectType, &e.SubjectID, &e.SubjectName, &e.IP, &detail); err != nil {
			return nil, err
		}
		e.At = fromUnix(at)
		if err := json.Unmarshal([]byte(detail), &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
