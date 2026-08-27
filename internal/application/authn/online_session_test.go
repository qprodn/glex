package authn

import (
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnrepo "glex/internal/domain/authn/repository"
	authnservice "glex/internal/domain/authn/service"

	authnv1 "glex/api/authn/v1"

	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeLoginSessionRepo struct {
	authnrepo.LoginSessionRepo
	sessions       []*authnentity.LoginSession
	logoutIDs      []string
	kickoutIDs     []string
	ownedUserID    int64
	ownedSessionID string
	err            error
}

func (r *fakeLoginSessionRepo) List(context.Context, ...authnrepo.ListOption) ([]*authnentity.LoginSession, error) {
	return r.sessions, r.err
}

func (r *fakeLoginSessionRepo) ListBySystemUser(context.Context, int64, ...authnrepo.ListOption) ([]*authnentity.LoginSession, error) {
	return r.sessions, r.err
}

func (r *fakeLoginSessionRepo) Logout(_ context.Context, sessionID string) error {
	r.logoutIDs = append(r.logoutIDs, sessionID)
	return r.err
}

func (r *fakeLoginSessionRepo) Kickout(_ context.Context, sessionID string) error {
	r.kickoutIDs = append(r.kickoutIDs, sessionID)
	return r.err
}

func (r *fakeLoginSessionRepo) LogoutOwned(_ context.Context, systemUserID int64, sessionID string) error {
	r.ownedUserID = systemUserID
	r.ownedSessionID = sessionID
	return r.err
}

func principalCtx(t *testing.T, systemUserID int64, sessionID string) context.Context {
	t.Helper()
	return authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(systemUserID, "alice", sessionID, nil, nil))
}

func TestLoginSessionLogoutAnonymousAndCurrentSession(t *testing.T) {
	repo := &fakeLoginSessionRepo{}
	app := NewLoginApplication(nil, nil, repo, nil, newFakeTransaction(), testLogger())
	if _, err := app.Logout(context.Background(), &emptypb.Empty{}); err != nil {
		t.Fatalf("anonymous Logout: %v", err)
	}
	if len(repo.logoutIDs) != 0 {
		t.Fatal("anonymous Logout reached the repository")
	}
	if _, err := app.Logout(principalCtx(t, 42, "current-session"), &emptypb.Empty{}); err != nil {
		t.Fatalf("authenticated Logout: %v", err)
	}
	if len(repo.logoutIDs) != 1 || repo.logoutIDs[0] != "current-session" {
		t.Fatalf("Logout IDs = %v, want only current-session", repo.logoutIDs)
	}
}

func TestLoginSessionLogoutPropagatesBackendFailure(t *testing.T) {
	repo := &fakeLoginSessionRepo{err: authndomain.ErrAuthBackendUnavailable}
	app := NewLoginApplication(nil, nil, repo, nil, newFakeTransaction(), testLogger())
	_, err := app.Logout(principalCtx(t, 42, "session-id"), &emptypb.Empty{})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Logout error = %v, want ErrAuthBackendUnavailable", err)
	}
}

func TestLoginSessionManagementUsesTargetedRepositoryOperations(t *testing.T) {
	repo := &fakeLoginSessionRepo{sessions: []*authnentity.LoginSession{{ID: "session-id", SystemUserID: 42}}}
	app := NewOnlineSessionApplication(repo, testLogger())
	if sessions, err := app.ListOnlineSessions(t.Context(), &authnv1.ListOnlineSessionsRequest{}); err != nil || len(sessions.GetSessions()) != 1 {
		t.Fatalf("ListOnlineSessions = %v, %v", sessions, err)
	}
	if sessions, err := app.ListMyOnlineSessions(principalCtx(t, 42, "session-id"), &emptypb.Empty{}); err != nil || len(sessions.GetSessions()) != 1 {
		t.Fatalf("ListMyOnlineSessions = %v, %v", sessions, err)
	}
	if _, err := app.KickoutOnlineSession(t.Context(), &authnv1.OnlineSessionRequest{TokenId: "session-id"}); err != nil {
		t.Fatalf("KickoutOnlineSession: %v", err)
	}
	if len(repo.kickoutIDs) != 1 || repo.kickoutIDs[0] != "session-id" {
		t.Fatalf("Kickout IDs = %v", repo.kickoutIDs)
	}
	if _, err := app.DeleteMyOnlineSession(principalCtx(t, 42, "session-id"), &authnv1.OnlineSessionRequest{TokenId: "session-id"}); err != nil {
		t.Fatalf("DeleteMyOnlineSession: %v", err)
	}
	if repo.ownedUserID != 42 || repo.ownedSessionID != "session-id" {
		t.Fatalf("ownership revoke = %d/%q", repo.ownedUserID, repo.ownedSessionID)
	}
}

func TestDeleteMyOnlineSessionRejectsForeignSession(t *testing.T) {
	repo := &fakeLoginSessionRepo{err: authndomain.ErrLoginSessionNotOwned}
	app := NewOnlineSessionApplication(repo, testLogger())
	_, err := app.DeleteMyOnlineSession(principalCtx(t, 42, "foreign-session"), &authnv1.OnlineSessionRequest{TokenId: "foreign-session"})
	if !errors.Is(err, authndomain.ErrLoginSessionNotOwned) {
		t.Fatalf("DeleteMyOnlineSession error = %v, want ErrLoginSessionNotOwned", err)
	}
}
