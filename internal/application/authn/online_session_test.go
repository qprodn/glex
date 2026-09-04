package authn

import (
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"

	authnv1 "glex/api/authn/v1"

	"context"
	"errors"
	"testing"
	"time"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeOnlineSessionService stands in for the Online Session domain service:
// it answers list calls from preloaded sessions and records the revocation
// calls, so tests can assert the DTO conversion and the ownership-gated
// operations. The embedded interface stays nil because the handlers touch
// nothing else.
type fakeOnlineSessionService struct {
	authnservice.OnlineSession
	sessions   []*authnentity.LoginSession
	listedBy   []int64
	kickoutIDs []string
	ownedUser  int64
	ownedID    string
	err        error
}

func (s *fakeOnlineSessionService) Get(_ context.Context, sessionID string) (*authnentity.LoginSession, error) {
	for _, session := range s.sessions {
		if session.ID == sessionID {
			return session, nil
		}
	}
	return nil, authndomain.ErrLoginSessionNotFound
}

func (s *fakeOnlineSessionService) List(context.Context, filtering.Filter, ordering.OrderBy, int, int) ([]*authnentity.LoginSession, error) {
	return s.sessions, s.err
}

func (s *fakeOnlineSessionService) ListBySystemUser(_ context.Context, systemUserID int64) ([]*authnentity.LoginSession, error) {
	s.listedBy = append(s.listedBy, systemUserID)
	return s.sessions, s.err
}

func (s *fakeOnlineSessionService) Kickout(_ context.Context, sessionID string) error {
	s.kickoutIDs = append(s.kickoutIDs, sessionID)
	return s.err
}

func (s *fakeOnlineSessionService) LogoutOwned(_ context.Context, systemUserID int64, sessionID string) error {
	s.ownedUser = systemUserID
	s.ownedID = sessionID
	return s.err
}

func principalCtx(t *testing.T, systemUserID int64, sessionID string) context.Context {
	t.Helper()
	return authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(systemUserID, "alice", sessionID, nil, nil))
}

func TestLoginSessionLogoutAnonymousAndCurrentSession(t *testing.T) {
	login := &fakeLoginService{}
	app := NewLoginApplication(login, nil, &fakeLoginLogWriter{}, nil, testLogger())
	if _, err := app.Logout(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("anonymous Logout: %v", err)
	}
	if len(login.logoutIDs) != 0 {
		t.Fatal("anonymous Logout reached the service")
	}
	if _, err := app.Logout(principalCtx(t, 42, "current-session"), &emptypb.Empty{}); err != nil {
		t.Fatalf("authenticated Logout: %v", err)
	}
	if len(login.logoutIDs) != 1 || login.logoutIDs[0] != "current-session" {
		t.Fatalf("Logout IDs = %v, want only current-session", login.logoutIDs)
	}
}

func TestLoginSessionLogoutPropagatesServiceFailure(t *testing.T) {
	login := &fakeLoginService{logoutErr: authndomain.ErrAuthBackendUnavailable}
	app := NewLoginApplication(login, nil, &fakeLoginLogWriter{}, nil, testLogger())
	_, err := app.Logout(principalCtx(t, 42, "session-id"), &emptypb.Empty{})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Logout error = %v, want ErrAuthBackendUnavailable", err)
	}
}

func TestOnlineSessionManagementUsesTargetedServiceOperations(t *testing.T) {
	loginTime := time.Unix(1_700_000_000, 0)
	svc := &fakeOnlineSessionService{sessions: []*authnentity.LoginSession{{
		ID:           "session-id",
		SystemUserID: 42,
		UserName:     "alice",
		DeviceType:   "mobile",
		IPAddress:    "192.0.2.10",
		Browser:      "Chrome",
		OS:           "iOS",
		LoginTime:    loginTime,
	}}}
	app := NewOnlineSessionApplication(svc, &fakeLoginLogWriter{}, testLogger())

	set, err := app.ListOnlineSessions(t.Context(), &authnv1.ListOnlineSessionsRequest{})
	if err != nil {
		t.Fatalf("ListOnlineSessions: %v", err)
	}
	if len(set.GetSessions()) != 1 {
		t.Fatalf("sessions = %v, want one entry", set.GetSessions())
	}
	got := set.GetSessions()[0]
	if got.GetTokenId() != "session-id" || got.GetUserName() != "alice" || got.GetDeviceType() != "mobile" ||
		got.GetIpaddr() != "192.0.2.10" || got.GetBrowser() != "Chrome" || got.GetOs() != "iOS" ||
		!got.GetLoginTime().AsTime().Equal(loginTime) {
		t.Fatalf("OnlineSession DTO = %#v, want every Login Session field mapped", got)
	}

	if _, err := app.ListMyOnlineSessions(context.Background(), &emptypb.Empty{}); !errors.Is(err, authndomain.ErrCredentialMissing) {
		t.Fatalf("ListMyOnlineSessions without a Principal = %v, want ErrCredentialMissing", err)
	}
	if _, err := app.ListMyOnlineSessions(principalCtx(t, 42, "session-id"), &emptypb.Empty{}); err != nil {
		t.Fatalf("ListMyOnlineSessions: %v", err)
	}
	if len(svc.listedBy) != 1 || svc.listedBy[0] != 42 {
		t.Fatalf("ListBySystemUser callers = %v, want [42] (only the authenticated Principal's)", svc.listedBy)
	}

	if _, err := app.KickoutOnlineSession(t.Context(), &authnv1.OnlineSessionRequest{TokenId: "session-id"}); err != nil {
		t.Fatalf("KickoutOnlineSession: %v", err)
	}
	if len(svc.kickoutIDs) != 1 || svc.kickoutIDs[0] != "session-id" {
		t.Fatalf("Kickout IDs = %v", svc.kickoutIDs)
	}

	if _, err := app.DeleteMyOnlineSession(principalCtx(t, 42, "session-id"), &authnv1.OnlineSessionRequest{TokenId: "session-id"}); err != nil {
		t.Fatalf("DeleteMyOnlineSession: %v", err)
	}
	if svc.ownedUser != 42 || svc.ownedID != "session-id" {
		t.Fatalf("ownership revoke = %d/%q", svc.ownedUser, svc.ownedID)
	}
}

func TestDeleteMyOnlineSessionRejectsForeignSession(t *testing.T) {
	svc := &fakeOnlineSessionService{err: authndomain.ErrLoginSessionNotOwned}
	app := NewOnlineSessionApplication(svc, &fakeLoginLogWriter{}, testLogger())
	_, err := app.DeleteMyOnlineSession(principalCtx(t, 42, "foreign-session"), &authnv1.OnlineSessionRequest{TokenId: "foreign-session"})
	if !errors.Is(err, authndomain.ErrLoginSessionNotOwned) {
		t.Fatalf("DeleteMyOnlineSession error = %v, want ErrLoginSessionNotOwned", err)
	}
}
