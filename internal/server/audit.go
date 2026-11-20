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

func (s *Server) appAccess(ctx context.Context, a *store.AuthRequest, userID string) (*store.Client, bool) {
	u, err := s.st.ActiveUser(ctx, userID)
	if err != nil {
		return nil, false
	}
	c, err := s.st.ClientByID(ctx, a.ClientID)
	if err != nil {
		return nil, false
	}
	allowed, err := s.st.UserMayUseClient(ctx, u, c)
	return c, err == nil && allowed
}

func (s *Server) denyApp(ctx context.Context, a *store.AuthRequest, userID string, c *store.Client) {
	_ = s.st.DenyAuthRequest(ctx, a.ID)
	if c != nil {
		s.audit(ctx, store.Event{Type: store.EvAppDenied, ActorID: userID, SubjectType: "app", SubjectID: c.ID})
	}
}

// approveAppWithAMR is approveApp with the authentication method recorded in
// the authorization request, so ID tokens carry pwd or webauthn.
func (s *Server) approveAppWithAMR(ctx context.Context, a *store.AuthRequest, userID string, authTime time.Time, amr []string) bool {
	c, allowed := s.appAccess(ctx, a, userID)
	if !allowed {
		s.denyApp(ctx, a, userID, c)
		return false
	}
	if err := s.st.ApproveAuthRequestWithConsent(ctx, a.ID, userID, authTime, amr, a.Scopes); err != nil {
		return false
	}
	s.audit(ctx, store.Event{Type: store.EvAppSignIn, ActorID: userID, SubjectType: "app", SubjectID: c.ID,
		Detail: map[string]any{"scopes": a.Scopes}})
	return true
}
