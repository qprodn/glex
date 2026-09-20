package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"glex/internal/domain/authn"
	"glex/internal/domain/authn/entity"
	"glex/internal/domain/authn/repository"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
)

// fakeLoginBackend answers the login backend port from preloaded fields and
// records the calls it received, so tests can assert which protocol steps a
// rule reaches. The transactional methods (Revalidate onwards) stay unused by
// VerifyPassword-focused tests.
type fakeLoginBackend struct {
	accounts   map[string]*entity.LoginAccount
	accountErr error
}

func (b *fakeLoginBackend) FindLoginAccount(_ context.Context, userName string) (*entity.LoginAccount, error) {
	if b.accountErr != nil {
		return nil, b.accountErr
	}
	account, ok := b.accounts[userName]
	if !ok {
		return nil, authn.ErrLoginAccountNotFound
	}
	return account, nil
}

func (b *fakeLoginBackend) Revalidate(_ context.Context, req repository.RevalidateRequest) (*entity.LoginAccount, error) {
	return &entity.LoginAccount{ID: req.SystemUserID, UserName: req.UserName}, nil
}

func (b *fakeLoginBackend) RecordLogin(context.Context, repository.RecordLoginRequest) error {
	return nil
}

func (b *fakeLoginBackend) IssueCredential(context.Context, repository.IssueCredentialRequest) (*entity.LoginResult, error) {
	return &entity.LoginResult{AccessToken: "secret"}, nil
}

func (b *fakeLoginBackend) DiscardCredential(context.Context, repository.DiscardCredentialRequest) error {
	return nil
}

// fakeCounterRepo answers the Redis failure-counter port from preloaded
// fields and records writes.
type fakeCounterRepo struct {
	remaining     time.Duration
	failRemaining time.Duration
	err           error
	failures      int
	cleared       []string
}

func (r *fakeCounterRepo) LockRemaining(context.Context, repository.LoginAttemptRequest) (time.Duration, error) {
	return r.remaining, r.err
}

func (r *fakeCounterRepo) RecordFailure(context.Context, repository.LoginAttemptRequest) (time.Duration, error) {
	r.failures++
	return r.failRemaining, r.err
}

func (r *fakeCounterRepo) ClearUserName(_ context.Context, userName string) error {
	r.cleared = append(r.cleared, userName)
	return r.err
}

// fakeSessionRepo records session-store writes; the embedded interface stays
// nil because the login service touches only Logout.
type fakeSessionRepo struct {
	repository.LoginSessionRepo
	logoutIDs []string
}

func (r *fakeSessionRepo) Logout(_ context.Context, sessionID string) error {
	r.logoutIDs = append(r.logoutIDs, sessionID)
	return nil
}

func newLoginServiceForTest(backend repository.AuthLoginBackend, counters repository.LoginCounterRepo) Login {
	return NewLoginService(backend, counters, &fakeSessionRepo{}, testLogger())
}

func accountWithPassword(userName, password string) *entity.LoginAccount {
	hash, err := HashPassword(password)
	if err != nil {
		panic(err)
	}
	return &entity.LoginAccount{ID: 42, UserName: userName, PasswordHash: hash}
}

func TestVerifyPasswordLockedGateBlocksBeforeTheBackend(t *testing.T) {
	counters := &fakeCounterRepo{remaining: 90*time.Second + time.Millisecond}
	backend := &fakeLoginBackend{accounts: map[string]*entity.LoginAccount{
		"alice": accountWithPassword("alice", "correct"),
	}}

	_, err := newLoginServiceForTest(backend, counters).VerifyPassword(t.Context(), VerifyPasswordRequest{UserName: "alice", Password: "correct", SourceIP: "192.0.2.10"})
	if kratoserrors.Reason(err) != "LOGIN_LOCKED" {
		t.Fatalf("reason = %q, want LOGIN_LOCKED", kratoserrors.Reason(err))
	}
	if got := kratoserrors.FromError(err).Metadata["remaining_seconds"]; got != "91" {
		t.Fatalf("remaining_seconds = %q, want 91 (rounded up)", got)
	}
	// A locked attempt neither verifies a password nor records a failure.
	if counters.failures != 0 {
		t.Fatalf("failures = %d, want 0 while locked", counters.failures)
	}
}

func TestVerifyPasswordUnknownAccountAndWrongPasswordAreIndistinguishable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		userName string
	}{
		{name: "unknown account", userName: "missing"},
		{name: "wrong password", userName: "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeLoginBackend{accounts: map[string]*entity.LoginAccount{
				"alice": accountWithPassword("alice", "correct"),
			}}
			counters := &fakeCounterRepo{}

			_, err := newLoginServiceForTest(backend, counters).VerifyPassword(t.Context(), VerifyPasswordRequest{UserName: tc.userName, Password: "wrong", SourceIP: "192.0.2.10"})
			if !errors.Is(err, authn.ErrInvalidCredentials) {
				t.Fatalf("VerifyPassword error = %v, want ErrInvalidCredentials", err)
			}
			if got := kratoserrors.Reason(err); got != "INVALID_CREDENTIALS" {
				t.Fatalf("reason = %q, want INVALID_CREDENTIALS", got)
			}
			if counters.failures != 1 {
				t.Fatalf("failures = %d, want 1 (every failure path records)", counters.failures)
			}
		})
	}
}

func TestVerifyPasswordAmbiguousAccountIsIndistinguishableFromInvalidCredentials(t *testing.T) {
	backend := &fakeLoginBackend{accountErr: authn.ErrLoginAccountAmbiguous}
	counters := &fakeCounterRepo{}

	_, err := newLoginServiceForTest(backend, counters).VerifyPassword(t.Context(), VerifyPasswordRequest{UserName: "alice", Password: "wrong", SourceIP: "192.0.2.10"})
	if !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatalf("VerifyPassword error = %v, want ErrInvalidCredentials", err)
	}
	if counters.failures != 1 {
		t.Fatalf("failures = %d, want 1 (an ambiguous identifier must not authenticate)", counters.failures)
	}
}

func TestVerifyPasswordBackendFailurePropagatesWithoutTouchingTheCounter(t *testing.T) {
	backend := &fakeLoginBackend{accountErr: errors.New("database unavailable")}
	counters := &fakeCounterRepo{}

	_, err := newLoginServiceForTest(backend, counters).VerifyPassword(t.Context(), VerifyPasswordRequest{UserName: "alice", Password: "wrong", SourceIP: "192.0.2.10"})
	if err == nil || !errors.Is(err, backend.accountErr) {
		t.Fatalf("VerifyPassword error = %v, want the propagated backend failure", err)
	}
	if counters.failures != 0 {
		t.Fatalf("failures = %d, want 0 (backend failures are not login failures)", counters.failures)
	}
}

func TestVerifyPasswordDisabledAccountRejectedOnlyAfterPasswordMatches(t *testing.T) {
	disabled := accountWithPassword("alice", "correct")
	disabled.Status = 1
	backend := &fakeLoginBackend{accounts: map[string]*entity.LoginAccount{"alice": disabled}}
	counters := &fakeCounterRepo{}
	svc := newLoginServiceForTest(backend, counters)

	if _, err := svc.VerifyPassword(t.Context(), VerifyPasswordRequest{UserName: "alice", Password: "wrong", SourceIP: "192.0.2.10"}); !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatalf("wrong password = %v, want ErrInvalidCredentials", err)
	}
	if counters.failures != 1 {
		t.Fatalf("failures = %d, want 1 (wrong password still records)", counters.failures)
	}

	if _, err := svc.VerifyPassword(t.Context(), VerifyPasswordRequest{UserName: "alice", Password: "correct", SourceIP: "192.0.2.10"}); !errors.Is(err, authn.ErrSystemUserDisabled) {
		t.Fatalf("correct password = %v, want ErrSystemUserDisabled", err)
	}
	// A matching password on a disabled account is a state problem, not a
	// failed attempt: the counter must stay where the wrong attempt left it.
	if counters.failures != 1 {
		t.Fatalf("failures = %d, want 1 (disabled is not a failed attempt)", counters.failures)
	}
}

func TestVerifyPasswordSuccessReturnsThePreTransactionAccount(t *testing.T) {
	alice := accountWithPassword("alice", "correct")
	backend := &fakeLoginBackend{accounts: map[string]*entity.LoginAccount{"alice": alice}}
	counters := &fakeCounterRepo{}

	got, err := newLoginServiceForTest(backend, counters).VerifyPassword(t.Context(), VerifyPasswordRequest{UserName: "alice", Password: "correct", SourceIP: "192.0.2.10"})
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if got != alice {
		t.Fatalf("account = %#v, want the loaded account verbatim", got)
	}
	if counters.failures != 0 || len(counters.cleared) != 0 {
		t.Fatalf("counter writes = %d/%v, want none on success", counters.failures, counters.cleared)
	}
}

func TestRecordFailureReportsTheRemainingLockWhenThresholdReached(t *testing.T) {
	counters := &fakeCounterRepo{failRemaining: 10 * time.Minute}

	err := NewLoginService(&fakeLoginBackend{}, counters, &fakeSessionRepo{}, testLogger()).RecordFailure(t.Context(), LoginAttemptRequest{UserName: "alice", SourceIP: "192.0.2.10"})
	if kratoserrors.Reason(err) != "LOGIN_LOCKED" {
		t.Fatalf("reason = %q, want LOGIN_LOCKED", kratoserrors.Reason(err))
	}
	if got := kratoserrors.FromError(err).Metadata["remaining_seconds"]; got != "600" {
		t.Fatalf("remaining_seconds = %q, want 600", got)
	}
}

func TestRecordFailureReturnsGenericReasonBelowThreshold(t *testing.T) {
	counters := &fakeCounterRepo{}

	err := NewLoginService(&fakeLoginBackend{}, counters, &fakeSessionRepo{}, testLogger()).RecordFailure(t.Context(), LoginAttemptRequest{UserName: "alice", SourceIP: "192.0.2.10"})
	if !errors.Is(err, authn.ErrInvalidCredentials) {
		t.Fatalf("RecordFailure error = %v, want ErrInvalidCredentials", err)
	}
	if counters.failures != 1 {
		t.Fatalf("failures = %d, want 1", counters.failures)
	}
}

func TestClearFailuresClearsTheUserNameCounter(t *testing.T) {
	counters := &fakeCounterRepo{}

	if err := NewLoginService(&fakeLoginBackend{}, counters, &fakeSessionRepo{}, testLogger()).ClearFailures(t.Context(), "alice"); err != nil {
		t.Fatalf("ClearFailures: %v", err)
	}
	if len(counters.cleared) != 1 || counters.cleared[0] != "alice" {
		t.Fatalf("cleared = %v, want [alice]", counters.cleared)
	}
}

func TestLogoutRevokesThroughTheSessionStore(t *testing.T) {
	sessions := &fakeSessionRepo{}

	if err := NewLoginService(&fakeLoginBackend{}, &fakeCounterRepo{}, sessions, testLogger()).Logout(t.Context(), "current-session"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if len(sessions.logoutIDs) != 1 || sessions.logoutIDs[0] != "current-session" {
		t.Fatalf("Logout IDs = %v, want [current-session]", sessions.logoutIDs)
	}
}
