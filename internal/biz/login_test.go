package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
)

type fakeAuthLoginRepo struct {
	issues []*LoginIssue
	result *LoginResult
	err    error
}

func (r *fakeAuthLoginRepo) Issue(_ context.Context, issue *LoginIssue) (*LoginResult, error) {
	r.issues = append(r.issues, issue)
	if r.err != nil {
		return nil, r.err
	}
	return r.result, nil
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
	uc := NewAuthUsecase(users, newFakeRoleRepo(), newFakeUserRoleRepo(), issuer, counters)
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
			if counters.failures != 1 || len(issuer.issues) != 0 {
				t.Fatalf("failures/issues = %d/%d, want 1/0", counters.failures, len(issuer.issues))
			}
		})
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
	if len(issuer.issues) != 0 {
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

func TestLoginSuccessClearsUsernameCounterAndDerivesClientMetadata(t *testing.T) {
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
	if len(issuer.issues) != 1 {
		t.Fatalf("issued %d times, want 1", len(issuer.issues))
	}
	issue := issuer.issues[0]
	if issue.DeviceType != "mobile" || issue.Browser != "Chrome" || issue.OS != "iOS" {
		t.Fatalf("metadata = %q/%q/%q, want mobile/Chrome/iOS", issue.DeviceType, issue.Browser, issue.OS)
	}
	if issue.LoginTime != time.Unix(1_700_000_000, 0) || issue.PasswordHash != hash {
		t.Fatalf("issue time/hash not preserved: %#v", issue)
	}
}

func TestLoginFailureThatReachesThresholdReturnsLocked(t *testing.T) {
	counters := &fakeLoginCounterRepo{failRemaining: 10 * time.Minute}
	_, err := newLoginUsecase(newFakeUserRepo(), &fakeAuthLoginRepo{}, counters).Login(t.Context(), &LoginRequest{UserName: "missing", Password: "wrong"})
	if kratoserrors.Reason(err) != "LOGIN_LOCKED" {
		t.Fatalf("reason = %q, want LOGIN_LOCKED", kratoserrors.Reason(err))
	}
}
