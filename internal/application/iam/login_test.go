package iam

import (
	. "glex/internal/domain/iam"

	"context"
	"errors"
	"testing"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
)

// fakeAuthLoginRepo records the login protocol's primitive calls in order and
// answers them from preloaded fields, so tests can assert the orchestration
// shape (lock → snapshot → record → issue, Redis last).
type fakeAuthLoginRepo struct {
	steps     []string
	locked    *User
	lockErr   error
	snapshot  *AuthorizationSnapshot
	snapErr   error
	recordIP  string
	recordAt  time.Time
	recordErr error
	result    *LoginResult
	issueErr  error
	issued    *LoginSession
	discarded []string
}

func (r *fakeAuthLoginRepo) Revalidate(_ context.Context, systemUserID int64, _, _ string) (*User, error) {
	r.steps = append(r.steps, "lock")
	if r.lockErr != nil {
		return nil, r.lockErr
	}
	if r.locked == nil {
		r.locked = &User{ID: systemUserID}
	}
	return r.locked, nil
}

func (r *fakeAuthLoginRepo) LoadSnapshot(_ context.Context, _ int64, _ string) (*AuthorizationSnapshot, error) {
	r.steps = append(r.steps, "snapshot")
	if r.snapErr != nil {
		return nil, r.snapErr
	}
	if r.snapshot == nil {
		r.snapshot = &AuthorizationSnapshot{}
	}
	return r.snapshot, nil
}

func (r *fakeAuthLoginRepo) RecordLogin(_ context.Context, _ int64, sourceIP string, loginTime time.Time) error {
	r.steps = append(r.steps, "record")
	r.recordIP = sourceIP
	r.recordAt = loginTime
	return r.recordErr
}

func (r *fakeAuthLoginRepo) IssueCredential(_ context.Context, _ *AuthorizationSnapshot, session *LoginSession) (*LoginResult, error) {
	r.steps = append(r.steps, "issue")
	r.issued = session
	if r.issueErr != nil {
		return nil, r.issueErr
	}
	if r.result == nil {
		r.result = &LoginResult{AccessToken: "secret"}
	}
	return r.result, nil
}

func (r *fakeAuthLoginRepo) DiscardCredential(_ context.Context, _ int64, bearerToken string) error {
	r.steps = append(r.steps, "discard")
	r.discarded = append(r.discarded, bearerToken)
	return nil
}

type fakeLoginCounterRepo struct {
	remaining     time.Duration
	failRemaining time.Duration
	err           error
	failures      int
	cleared       []string
}

func (r *fakeLoginCounterRepo) LockRemaining(context.Context, string, string) (time.Duration, error) {
	return r.remaining, r.err
}

func (r *fakeLoginCounterRepo) RecordFailure(context.Context, string, string) (time.Duration, error) {
	r.failures++
	return r.failRemaining, r.err
}

func (r *fakeLoginCounterRepo) ClearUserName(_ context.Context, userName string) error {
	r.cleared = append(r.cleared, userName)
	return r.err
}

func newLoginUsecase(users *fakeUserRepo, issuer *fakeAuthLoginRepo, counters *fakeLoginCounterRepo) *AuthUsecase {
	return newLoginUsecaseWithTx(users, issuer, counters, newFakeTransaction())
}

func newLoginUsecaseWithTx(users *fakeUserRepo, issuer *fakeAuthLoginRepo, counters *fakeLoginCounterRepo, tx *fakeTransaction) *AuthUsecase {
	uc := NewAuthUsecase(users, newFakeRoleRepo(), newFakeUserRoleRepo(), issuer, counters, &stubSessionRepo{}, new(fakeSessionRevoker), tx, testLogger())
	uc.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return uc
}

func TestLoginUnknownUserAndBadPasswordAreIndistinguishable(t *testing.T) {
	hash, err := hashPassword("correct")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	users := newFakeUserRepo()
	users.byName["alice"] = &User{ID: 42, UserName: "alice", Password: hash}

	for _, tc := range []struct {
		name     string
		userName string
	}{
		{name: "unknown", userName: "missing"},
		{name: "bad password", userName: "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issuer := &fakeAuthLoginRepo{}
			counters := &fakeLoginCounterRepo{}
			_, err := newLoginUsecase(users, issuer, counters).Login(t.Context(), &LoginRequest{
				UserName: tc.userName,
				Password: "wrong",
				SourceIP: "192.0.2.10",
			})
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
			}
			if got := kratoserrors.Reason(err); got != "INVALID_CREDENTIALS" {
				t.Fatalf("reason = %q, want INVALID_CREDENTIALS", got)
			}
			if counters.failures != 1 || len(issuer.steps) != 0 {
				t.Fatalf("failures/steps = %d/%v, want 1/[]", counters.failures, issuer.steps)
			}
		})
	}
}

func TestLoginAmbiguousUserNameIsIndistinguishableFromInvalidCredentials(t *testing.T) {
	users := newFakeUserRepo()
	users.userNameErr = ErrUserNameAmbiguous
	issuer := &fakeAuthLoginRepo{}
	counters := &fakeLoginCounterRepo{}

	_, err := newLoginUsecase(users, issuer, counters).Login(t.Context(), &LoginRequest{
		UserName: "alice", Password: "wrong", SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	if counters.failures != 1 || len(issuer.steps) != 0 {
		t.Fatalf("ambiguous UserName must not authenticate: failures=%d steps=%v", counters.failures, issuer.steps)
	}
}

func TestLoginDisabledSystemUserRejectedOnlyAfterPasswordMatches(t *testing.T) {
	hash, err := hashPassword("correct")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	users := newFakeUserRepo()
	users.byName["alice"] = &User{ID: 42, UserName: "alice", Password: hash, Status: 1}
	issuer := &fakeAuthLoginRepo{}
	counters := &fakeLoginCounterRepo{}
	uc := newLoginUsecase(users, issuer, counters)

	if _, err := uc.Login(t.Context(), &LoginRequest{UserName: "alice", Password: "wrong"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password = %v, want ErrInvalidCredentials", err)
	}
	if _, err := uc.Login(t.Context(), &LoginRequest{UserName: "alice", Password: "correct"}); !errors.Is(err, ErrSystemUserDisabled) {
		t.Fatalf("correct password = %v, want ErrSystemUserDisabled", err)
	}
	if len(issuer.steps) != 0 {
		t.Fatal("disabled System User must not create a Login Session")
	}
}

func TestLoginLockedCarriesRemainingDuration(t *testing.T) {
	counters := &fakeLoginCounterRepo{remaining: 90*time.Second + time.Millisecond}
	_, err := newLoginUsecase(newFakeUserRepo(), &fakeAuthLoginRepo{}, counters).Login(t.Context(), &LoginRequest{UserName: "alice"})
	if kratoserrors.Reason(err) != "LOGIN_LOCKED" {
		t.Fatalf("reason = %q, want LOGIN_LOCKED", kratoserrors.Reason(err))
	}
	if got := kratoserrors.FromError(err).Metadata["remaining_seconds"]; got != "91" {
		t.Fatalf("remaining_seconds = %q, want 91", got)
	}
}

func TestLoginSuccessRunsDatabaseStepsThenRedisLast(t *testing.T) {
	hash, err := hashPassword("correct")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	users := newFakeUserRepo()
	users.byName["alice"] = &User{ID: 42, UserName: "alice", Password: hash}
	issuer := &fakeAuthLoginRepo{result: &LoginResult{AccessToken: "secret", ExpiresIn: 7 * 24 * time.Hour}}
	counters := &fakeLoginCounterRepo{}
	uc := newLoginUsecase(users, issuer, counters)

	got, err := uc.Login(t.Context(), &LoginRequest{
		UserName:  " alice ",
		Password:  "correct",
		SourceIP:  "192.0.2.10",
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0 Mobile/15E148 Safari/604.1",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.AccessToken != "secret" || len(counters.cleared) != 1 || counters.cleared[0] != "alice" {
		t.Fatalf("result/cleared = %#v/%v", got, counters.cleared)
	}
	// Orchestration order: lock → snapshot → record (PostgreSQL) → issue
	// (Redis, deliberately last); nothing after it inside the closure.
	assertSteps(t, &issuer.steps, "lock", "snapshot", "record", "issue")
	if issuer.issued == nil {
		t.Fatal("no Login Session was registered")
	}
	if issuer.issued.DeviceType != "mobile" || issuer.issued.Browser != "Chrome" || issuer.issued.OS != "iOS" {
		t.Fatalf("metadata = %q/%q/%q, want mobile/Chrome/iOS", issuer.issued.DeviceType, issuer.issued.Browser, issuer.issued.OS)
	}
	if issuer.issued.LoginTime != time.Unix(1_700_000_000, 0) || issuer.issued.IPAddress != "192.0.2.10" || issuer.issued.SystemUserID != 42 {
		t.Fatalf("session = %#v", issuer.issued)
	}
	if issuer.recordIP != "192.0.2.10" || issuer.recordAt != time.Unix(1_700_000_000, 0) {
		t.Fatalf("recorded login = %q/%v", issuer.recordIP, issuer.recordAt)
	}
}

func TestLoginRevalidationFailureFeedsTheCounter(t *testing.T) {
	hash, err := hashPassword("correct")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	users := newFakeUserRepo()
	users.byName["alice"] = &User{ID: 42, UserName: "alice", Password: hash}
	issuer := &fakeAuthLoginRepo{lockErr: ErrInvalidCredentials}
	counters := &fakeLoginCounterRepo{}

	_, err = newLoginUsecase(users, issuer, counters).Login(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	if counters.failures != 1 {
		t.Fatalf("failures = %d, want 1 (stale hash counts as a failed attempt)", counters.failures)
	}
	if len(issuer.steps) != 1 || issuer.steps[0] != "lock" {
		t.Fatalf("steps = %v, want [lock]", issuer.steps)
	}
}

func TestLoginRedisFailureRollsBackWithoutCompensation(t *testing.T) {
	hash, err := hashPassword("correct")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	users := newFakeUserRepo()
	users.byName["alice"] = &User{ID: 42, UserName: "alice", Password: hash}
	issuer := &fakeAuthLoginRepo{issueErr: ErrAuthBackendUnavailable}
	counters := &fakeLoginCounterRepo{}

	_, err = newLoginUsecase(users, issuer, counters).Login(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, ErrAuthBackendUnavailable) {
		t.Fatalf("Login error = %v, want ErrAuthBackendUnavailable", err)
	}
	// The credential was never fully issued, so the data wrapper cleaned up
	// its partial writes itself — the usecase must not discard again.
	if len(issuer.discarded) != 0 {
		t.Fatalf("discarded = %v, want none on Redis failure", issuer.discarded)
	}
	assertSteps(t, &issuer.steps, "lock", "snapshot", "record", "issue")
}

func TestLoginCommitFailureDiscardsTheCredential(t *testing.T) {
	hash, err := hashPassword("correct")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	users := newFakeUserRepo()
	users.byName["alice"] = &User{ID: 42, UserName: "alice", Password: hash}
	issuer := &fakeAuthLoginRepo{result: &LoginResult{AccessToken: "leaked"}}
	counters := &fakeLoginCounterRepo{}
	tx := &fakeTransaction{err: ErrAuthBackendUnavailable} // commit fails after the body

	_, err = newLoginUsecaseWithTx(users, issuer, counters, tx).Login(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, ErrAuthBackendUnavailable) {
		t.Fatalf("Login error = %v, want the commit failure", err)
	}
	if len(issuer.discarded) != 1 || issuer.discarded[0] != "leaked" {
		t.Fatalf("discarded = %v, want [leaked]", issuer.discarded)
	}
	assertSteps(t, &issuer.steps, "lock", "snapshot", "record", "issue", "discard")
}

func TestLoginFailureThatReachesThresholdReturnsLocked(t *testing.T) {
	counters := &fakeLoginCounterRepo{failRemaining: 10 * time.Minute}
	_, err := newLoginUsecase(newFakeUserRepo(), &fakeAuthLoginRepo{}, counters).Login(t.Context(), &LoginRequest{UserName: "missing", Password: "wrong"})
	if kratoserrors.Reason(err) != "LOGIN_LOCKED" {
		t.Fatalf("reason = %q, want LOGIN_LOCKED", kratoserrors.Reason(err))
	}
}
