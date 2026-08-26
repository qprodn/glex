package authn

import (
	authndomain "glex/internal/domain/authn"

	"context"
	"errors"
	"testing"
)

type fakeLoginSessionRepo struct {
	authndomain.LoginSessionRepo
	sessions       []*authndomain.LoginSession
	logoutIDs      []string
	kickoutIDs     []string
	ownedUserID    int64
	ownedSessionID string
	err            error
}

func (r *fakeLoginSessionRepo) List(context.Context, ...authndomain.ListOption) ([]*authndomain.LoginSession, error) {
	return r.sessions, r.err
}

func (r *fakeLoginSessionRepo) ListBySystemUser(context.Context, int64, ...authndomain.ListOption) ([]*authndomain.LoginSession, error) {
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

func TestLoginSessionLogoutAnonymousAndCurrentSession(t *testing.T) {
	repo := &fakeLoginSessionRepo{}
	uc := NewLoginUsecase(nil, nil, repo, nil, newFakeTransaction(), testLogger())
	if err := uc.Logout(t.Context(), nil); err != nil {
		t.Fatalf("anonymous Logout: %v", err)
	}
	if len(repo.logoutIDs) != 0 {
		t.Fatal("anonymous Logout reached the repository")
	}
	principal := authndomain.NewPrincipal(42, "alice", "current-session", nil, nil)
	if err := uc.Logout(t.Context(), principal); err != nil {
		t.Fatalf("authenticated Logout: %v", err)
	}
	if len(repo.logoutIDs) != 1 || repo.logoutIDs[0] != "current-session" {
		t.Fatalf("Logout IDs = %v, want only current-session", repo.logoutIDs)
	}
}

func TestLoginSessionLogoutPropagatesBackendFailure(t *testing.T) {
	repo := &fakeLoginSessionRepo{err: authndomain.ErrAuthBackendUnavailable}
	uc := NewLoginUsecase(nil, nil, repo, nil, newFakeTransaction(), testLogger())
	err := uc.Logout(t.Context(), authndomain.NewPrincipal(42, "alice", "session-id", nil, nil))
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Logout error = %v, want ErrAuthBackendUnavailable", err)
	}
}

func TestLoginSessionManagementUsesTargetedRepositoryOperations(t *testing.T) {
	repo := &fakeLoginSessionRepo{sessions: []*authndomain.LoginSession{{ID: "session-id", SystemUserID: 42}}}
	uc := NewLoginSessionUsecase(repo, testLogger())
	if sessions, err := uc.ListOnlineSessions(t.Context()); err != nil || len(sessions) != 1 {
		t.Fatalf("ListOnlineSessions = %v, %v", sessions, err)
	}
	if sessions, err := uc.ListSystemUserSessions(t.Context(), 42); err != nil || len(sessions) != 1 {
		t.Fatalf("ListSystemUserSessions = %v, %v", sessions, err)
	}
	if err := uc.KickoutOnlineSession(t.Context(), "session-id"); err != nil {
		t.Fatalf("KickoutOnlineSession: %v", err)
	}
	if len(repo.kickoutIDs) != 1 || repo.kickoutIDs[0] != "session-id" {
		t.Fatalf("Kickout IDs = %v", repo.kickoutIDs)
	}
	if err := uc.DeleteSystemUserSession(t.Context(), 42, "session-id"); err != nil {
		t.Fatalf("DeleteSystemUserSession: %v", err)
	}
	if repo.ownedUserID != 42 || repo.ownedSessionID != "session-id" {
		t.Fatalf("ownership revoke = %d/%q", repo.ownedUserID, repo.ownedSessionID)
	}
}

func TestDeleteSystemUserSessionRejectsForeignSession(t *testing.T) {
	repo := &fakeLoginSessionRepo{err: authndomain.ErrLoginSessionNotOwned}
	err := NewLoginSessionUsecase(repo, testLogger()).DeleteSystemUserSession(t.Context(), 42, "foreign-session")
	if !errors.Is(err, authndomain.ErrLoginSessionNotOwned) {
		t.Fatalf("DeleteSystemUserSession error = %v, want ErrLoginSessionNotOwned", err)
	}
}
