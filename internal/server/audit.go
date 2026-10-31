package server

import (
	"context"
	"time"

	"xauth/internal/store"
)

// audit records an event. A failure is logged but never fails the request:
// signing in matters more than the log line that records it.
func (s *Server) audit(ctx context.Context, e store.Event) {
	if err := s.st.Record(ctx, e); err != nil {
		s.log.Error("audit log", "type", e.Type, "err", err)
	}
}

func idOf(u *store.User) string {
	if u == nil {
		return ""
	}
	return u.ID
}

// approveApp records who signed in to the app. It returns false when the
// user may not use the app or the request was already decided.
func (s *Server) approveApp(ctx context.Context, a *store.AuthRequest, userID string, authTime time.Time) bool {
	return s.approveAppWithAMR(ctx, a, userID, authTime, passwordAMR)
}

// approveAppWithAMR is approveApp with the authentication method recorded in
// the authorization request, so ID tokens carry pwd or webauthn.
func (s *Server) approveAppWithAMR(ctx context.Context, a *store.AuthRequest, userID string, authTime time.Time, amr []string) bool {
	u, err := s.st.ActiveUser(ctx, userID)
	if err != nil {
		return false
	}
	c, err := s.st.ClientByID(ctx, a.ClientID)
	if err != nil {
		return false
	}
	allowed, err := s.st.UserMayUseClient(ctx, u, c)
	if err != nil || !allowed {
		_ = s.st.DenyAuthRequest(ctx, a.ID)
		s.audit(ctx, store.Event{Type: store.EvAppDenied, ActorID: userID, SubjectType: "app", SubjectID: c.ID})
		return false
	}
	if err := s.st.ApproveAuthRequest(ctx, a.ID, userID, authTime, amr); err != nil {
		return false
	}
	s.audit(ctx, store.Event{Type: store.EvAppSignIn, ActorID: userID, SubjectType: "app", SubjectID: c.ID,
		Detail: map[string]any{"scopes": a.Scopes}})
	return true
}
