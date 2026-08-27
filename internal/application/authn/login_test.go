package authn

import (
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"

	"context"
	"errors"
	"testing"
	"time"

	"glex/internal/crossdomain/access"
	accessmodel "glex/internal/crossdomain/access/model"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
)

// fakeAuthLoginRepo records the login protocol's primitive calls in order and
// answers them from preloaded fields, so tests can assert the orchestration
// shape (lock → snapshot → record → issue, Redis last). The pre-transaction
// account lookup answers from accounts/accountErr.
type fakeAuthLoginRepo struct {
	accounts   map[string]*authnentity.LoginAccount
	accountErr error

	steps          []string
	locked         *authnentity.LoginAccount
	lockErr        error
	recordIP       string
	recordAt       time.Time
	recordErr      error
	result         *authnentity.LoginResult
	issueErr       error
	issued         *authnentity.LoginSession
	issuedSnapshot *authnentity.AuthorizationSnapshot
	discarded      []string
}

func (r *fakeAuthLoginRepo) FindLoginAccount(_ context.Context, userName string) (*authnentity.LoginAccount, error) {
	if r.accountErr != nil {
		return nil, r.accountErr
	}
	account, ok := r.accounts[userName]
	if !ok {
		return nil, authndomain.ErrLoginAccountNotFound
	}
	return account, nil
}

func (r *fakeAuthLoginRepo) Revalidate(_ context.Context, systemUserID int64, userName, _ string) (*authnentity.LoginAccount, error) {
	r.steps = append(r.steps, "lock")
	if r.lockErr != nil {
		return nil, r.lockErr
	}
	if r.locked == nil {
		r.locked = &authnentity.LoginAccount{ID: systemUserID, UserName: userName}
	}
	return r.locked, nil
}

func (r *fakeAuthLoginRepo) RecordLogin(_ context.Context, _ int64, sourceIP string, loginTime time.Time) error {
	r.steps = append(r.steps, "record")
	r.recordIP = sourceIP
	r.recordAt = loginTime
	return r.recordErr
}

func (r *fakeAuthLoginRepo) IssueCredential(_ context.Context, snapshot *authnentity.AuthorizationSnapshot, session *authnentity.LoginSession) (*authnentity.LoginResult, error) {
	r.steps = append(r.steps, "issue")
	r.issuedSnapshot = snapshot
	r.issued = session
	if r.issueErr != nil {
		return nil, r.issueErr
	}
	if r.result == nil {
		r.result = &authnentity.LoginResult{AccessToken: "secret"}
	}
	return r.result, nil
}

func (r *fakeAuthLoginRepo) DiscardCredential(_ context.Context, _ int64, bearerToken string) error {
	r.steps = append(r.steps, "discard")
	r.discarded = append(r.discarded, bearerToken)
	return nil
}

// fakeAuthorizer stands in for the access crossdomain contract: it records
// the snapshot step and answers from preloaded fields.
type fakeAuthorizer struct {
	steps      *[]string
	resolution *accessmodel.AuthorizationResolution
	err        error
}

func (a *fakeAuthorizer) ResolveAuthorization(_ context.Context, _ int64) (*accessmodel.AuthorizationResolution, error) {
	if a.steps != nil {
		*a.steps = append(*a.steps, "snapshot")
	}
	if a.err != nil {
		return nil, a.err
	}
	if a.resolution == nil {
		a.resolution = &accessmodel.AuthorizationResolution{}
	}
	return a.resolution, nil
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

func newLoginUsecase(issuer *fakeAuthLoginRepo, counters *fakeLoginCounterRepo) *LoginUsecase {
	return newLoginUsecaseWithTx(issuer, counters, newFakeTransaction())
}

func newLoginUsecaseWithTx(issuer *fakeAuthLoginRepo, counters *fakeLoginCounterRepo, tx *fakeTransaction) *LoginUsecase {
	uc := NewLoginUsecase(issuer, counters, &stubSessionRepo{}, &fakeAuthorizer{steps: &issuer.steps}, tx, testLogger())
	uc.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return uc
}

// newLoginUsecaseWithAuthorizer lets a test inject its own access contract
// fake (step sharing, resolution content, or failure).
func newLoginUsecaseWithAuthorizer(issuer *fakeAuthLoginRepo, counters *fakeLoginCounterRepo, authorizer access.AuthorizationResolver) *LoginUsecase {
	uc := NewLoginUsecase(issuer, counters, &stubSessionRepo{}, authorizer, newFakeTransaction(), testLogger())
	uc.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return uc
}

func accountWithPassword(userName, password string) *authnentity.LoginAccount {
	hash, err := authnservice.HashPassword(password)
	if err != nil {
		panic(err)
	}
	return &authnentity.LoginAccount{ID: 42, UserName: userName, PasswordHash: hash}
}

func TestLoginUnknownAccountAndBadPasswordAreIndistinguishable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		userName string
	}{
		{name: "unknown", userName: "missing"},
		{name: "bad password", userName: "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issuer := &fakeAuthLoginRepo{accounts: map[string]*authnentity.LoginAccount{
				"alice": accountWithPassword("alice", "correct"),
			}}
			counters := &fakeLoginCounterRepo{}
			_, err := newLoginUsecase(issuer, counters).Login(t.Context(), &LoginRequest{
				UserName: tc.userName,
				Password: "wrong",
				SourceIP: "192.0.2.10",
			})
			if !errors.Is(err, authndomain.ErrInvalidCredentials) {
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

func TestLoginAmbiguousAccountIsIndistinguishableFromInvalidCredentials(t *testing.T) {
	issuer := &fakeAuthLoginRepo{accountErr: authndomain.ErrLoginAccountAmbiguous}
	counters := &fakeLoginCounterRepo{}

	_, err := newLoginUsecase(issuer, counters).Login(t.Context(), &LoginRequest{
		UserName: "alice", Password: "wrong", SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	if counters.failures != 1 || len(issuer.steps) != 0 {
		t.Fatalf("ambiguous account must not authenticate: failures=%d steps=%v", counters.failures, issuer.steps)
	}
}

func TestLoginDisabledAccountRejectedOnlyAfterPasswordMatches(t *testing.T) {
	disabled := accountWithPassword("alice", "correct")
	disabled.Status = 1
	issuer := &fakeAuthLoginRepo{accounts: map[string]*authnentity.LoginAccount{"alice": disabled}}
	counters := &fakeLoginCounterRepo{}
	uc := newLoginUsecase(issuer, counters)

	if _, err := uc.Login(t.Context(), &LoginRequest{UserName: "alice", Password: "wrong"}); !errors.Is(err, authndomain.ErrInvalidCredentials) {
		t.Fatalf("wrong password = %v, want ErrInvalidCredentials", err)
	}
	if _, err := uc.Login(t.Context(), &LoginRequest{UserName: "alice", Password: "correct"}); !errors.Is(err, authndomain.ErrSystemUserDisabled) {
		t.Fatalf("correct password = %v, want ErrSystemUserDisabled", err)
	}
	if len(issuer.steps) != 0 {
		t.Fatal("disabled account must not create a Login Session")
	}
}

func TestLoginLockedCarriesRemainingDuration(t *testing.T) {
	counters := &fakeLoginCounterRepo{remaining: 90*time.Second + time.Millisecond}
	_, err := newLoginUsecase(&fakeAuthLoginRepo{}, counters).Login(t.Context(), &LoginRequest{UserName: "alice"})
	if kratoserrors.Reason(err) != "LOGIN_LOCKED" {
		t.Fatalf("reason = %q, want LOGIN_LOCKED", kratoserrors.Reason(err))
	}
	if got := kratoserrors.FromError(err).Metadata["remaining_seconds"]; got != "91" {
		t.Fatalf("remaining_seconds = %q, want 91", got)
	}
}

func TestLoginSuccessRunsDatabaseStepsThenRedisLast(t *testing.T) {
	issuer := &fakeAuthLoginRepo{
		accounts: map[string]*authnentity.LoginAccount{"alice": accountWithPassword("alice", "correct")},
		result:   &authnentity.LoginResult{AccessToken: "secret", ExpiresIn: 7 * 24 * time.Hour},
	}
	counters := &fakeLoginCounterRepo{}
	uc := newLoginUsecase(issuer, counters)

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
	issuer := &fakeAuthLoginRepo{
		accounts: map[string]*authnentity.LoginAccount{"alice": accountWithPassword("alice", "correct")},
		lockErr:  authndomain.ErrInvalidCredentials,
	}
	counters := &fakeLoginCounterRepo{}

	_, err := newLoginUsecase(issuer, counters).Login(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrInvalidCredentials) {
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
	issuer := &fakeAuthLoginRepo{
		accounts: map[string]*authnentity.LoginAccount{"alice": accountWithPassword("alice", "correct")},
		issueErr: authndomain.ErrAuthBackendUnavailable,
	}
	counters := &fakeLoginCounterRepo{}

	_, err := newLoginUsecase(issuer, counters).Login(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
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
	issuer := &fakeAuthLoginRepo{
		accounts: map[string]*authnentity.LoginAccount{"alice": accountWithPassword("alice", "correct")},
		result:   &authnentity.LoginResult{AccessToken: "leaked"},
	}
	counters := &fakeLoginCounterRepo{}
	tx := &fakeTransaction{err: authndomain.ErrAuthBackendUnavailable} // commit fails after the body

	_, err := newLoginUsecaseWithTx(issuer, counters, tx).Login(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Login error = %v, want the commit failure", err)
	}
	if len(issuer.discarded) != 1 || issuer.discarded[0] != "leaked" {
		t.Fatalf("discarded = %v, want [leaked]", issuer.discarded)
	}
	assertSteps(t, &issuer.steps, "lock", "snapshot", "record", "issue", "discard")
}

func TestLoginFailureThatReachesThresholdReturnsLocked(t *testing.T) {
	counters := &fakeLoginCounterRepo{failRemaining: 10 * time.Minute}
	_, err := newLoginUsecase(&fakeAuthLoginRepo{}, counters).Login(t.Context(), &LoginRequest{UserName: "missing", Password: "wrong"})
	if kratoserrors.Reason(err) != "LOGIN_LOCKED" {
		t.Fatalf("reason = %q, want LOGIN_LOCKED", kratoserrors.Reason(err))
	}
}

func TestLoginBackendFailurePropagatesWithoutTouchingTheCounter(t *testing.T) {
	issuer := &fakeAuthLoginRepo{accountErr: errors.New("database unavailable")}
	counters := &fakeLoginCounterRepo{}

	_, err := newLoginUsecase(issuer, counters).Login(t.Context(), &LoginRequest{
		UserName: "alice", Password: "wrong", SourceIP: "192.0.2.10",
	})
	if err == nil || !errors.Is(err, issuer.accountErr) {
		t.Fatalf("Login error = %v, want the propagated backend failure", err)
	}
	if counters.failures != 0 {
		t.Fatalf("failures = %d, want 0 (backend failures are not login failures)", counters.failures)
	}
}

func TestLoginResolutionFailureIsAuthBackendUnavailable(t *testing.T) {
	issuer := &fakeAuthLoginRepo{
		accounts: map[string]*authnentity.LoginAccount{"alice": accountWithPassword("alice", "correct")},
	}
	counters := &fakeLoginCounterRepo{}

	_, err := newLoginUsecaseWithAuthorizer(issuer, counters, &fakeAuthorizer{steps: &issuer.steps, err: errors.New("access unavailable")}).Login(t.Context(), &LoginRequest{
		UserName: "alice", Password: "correct", SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Login error = %v, want ErrAuthBackendUnavailable", err)
	}
	if counters.failures != 0 {
		t.Fatalf("failures = %d, want 0 (resolution failures are not login failures)", counters.failures)
	}
	// Resolution happens between revalidation and the last-login write; the
	// credential is never issued.
	assertSteps(t, &issuer.steps, "lock", "snapshot")
}

func TestLoginFillsSnapshotFromTheAccessContract(t *testing.T) {
	issuer := &fakeAuthLoginRepo{
		accounts: map[string]*authnentity.LoginAccount{"alice": accountWithPassword("alice", "correct")},
		result:   &authnentity.LoginResult{AccessToken: "secret"},
	}
	counters := &fakeLoginCounterRepo{}
	authorizer := &fakeAuthorizer{resolution: &accessmodel.AuthorizationResolution{
		RoleKeys:    []string{"viewer"},
		Permissions: []string{"system:user:list"},
	}}
	uc := newLoginUsecaseWithAuthorizer(issuer, counters, authorizer)

	if _, err := uc.Login(t.Context(), &LoginRequest{UserName: "alice", Password: "correct"}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	snapshot := issuer.issuedSnapshot
	if snapshot == nil {
		t.Fatal("no Authorization Snapshot was issued")
	}
	// Identity is authn's own carrier data; authorization content is the
	// contract's answer, passed through verbatim.
	if snapshot.SystemUserID != 42 || snapshot.UserName != "alice" {
		t.Fatalf("carrier = %d/%q, want 42/alice", snapshot.SystemUserID, snapshot.UserName)
	}
	if len(snapshot.RoleKeys) != 1 || snapshot.RoleKeys[0] != "viewer" {
		t.Fatalf("RoleKeys = %v", snapshot.RoleKeys)
	}
	if len(snapshot.Permissions) != 1 || snapshot.Permissions[0] != "system:user:list" {
		t.Fatalf("Permissions = %v", snapshot.Permissions)
	}
}
