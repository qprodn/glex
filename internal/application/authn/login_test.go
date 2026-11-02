package authn

import (
	"context"
	"errors"
	"testing"
	"time"

	authnv1 "glex/api/authn/v1"
	"glex/internal/crossdomain/access"
	accessmodel "glex/internal/crossdomain/access/model"
	auditcontract "glex/internal/crossdomain/audit"
	auditmodel "glex/internal/crossdomain/audit/model"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"

	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeLoginLogWriter stands in for the audit crossdomain contract: it
// records every login-log write so tests can assert the recorded outcomes,
// and can fail on demand to pin the best-effort behavior (ADR 0012).
type fakeLoginLogWriter struct {
	auditcontract.LoginLogWriter
	logs []*auditmodel.LoginLog
	err  error
}

func (w *fakeLoginLogWriter) RecordLoginLog(_ context.Context, request *auditmodel.LoginLog) error {
	if w.err != nil {
		return w.err
	}
	w.logs = append(w.logs, request)
	return nil
}

// fakeLoginService stands in for the login domain service: it records every
// orchestration step in order (challenge → verify → clear → lock → snapshot →
// record → issue, Redis last) and answers each call from preloaded fields, so
// tests can assert the sequencing, the snapshot fill, and the commit-failure
// compensation without touching the domain rules themselves.
type fakeLoginService struct {
	account   *authnentity.LoginAccount
	verifyErr error

	consumeErr        error
	consumedID        string
	consumedAnswer    string
	issuedChallenge   *authnentity.LoginChallenge
	issueChallengeErr error

	verifyName string
	steps      []string
	cleared    []string
	clearErr   error
	failures   int

	locked         *authnentity.LoginAccount
	lockErr        error
	recordIP       string
	recordAt       time.Time
	result         *authnentity.LoginResult
	issueErr       error
	issued         *authnentity.LoginSession
	issuedSnapshot *authnentity.AuthorizationSnapshot
	discarded      []string
	logoutIDs      []string
	logoutErr      error
}

func (s *fakeLoginService) IssueChallenge(context.Context) (*authnentity.LoginChallenge, error) {
	s.steps = append(s.steps, "issue-challenge")
	if s.issueChallengeErr != nil {
		return nil, s.issueChallengeErr
	}
	if s.issuedChallenge == nil {
		s.issuedChallenge = &authnentity.LoginChallenge{ID: "cid", Image: []byte{0x89, 0x50}}
	}
	return s.issuedChallenge, nil
}

func (s *fakeLoginService) ConsumeChallenge(_ context.Context, req authnservice.ConsumeChallengeRequest) error {
	s.steps = append(s.steps, "challenge")
	s.consumedID = req.ChallengeID
	s.consumedAnswer = req.Answer
	return s.consumeErr
}

func (s *fakeLoginService) VerifyPassword(_ context.Context, req authnservice.VerifyPasswordRequest) (*authnentity.LoginAccount, error) {
	s.steps = append(s.steps, "verify")
	s.verifyName = req.UserName
	if s.verifyErr != nil {
		return nil, s.verifyErr
	}
	if s.account == nil {
		s.account = &authnentity.LoginAccount{ID: 42, UserName: req.UserName, PasswordHash: "hash"}
	}
	return s.account, nil
}

// RecordFailure answers with the below-threshold normalized outcome, like the
// domain service does once the attempt is recorded.
func (s *fakeLoginService) RecordFailure(_ context.Context, _ authnservice.LoginAttemptRequest) error {
	s.steps = append(s.steps, "fail")
	s.failures++
	return authndomain.ErrInvalidCredentials
}

func (s *fakeLoginService) ClearFailures(_ context.Context, userName string) error {
	s.steps = append(s.steps, "clear")
	s.cleared = append(s.cleared, userName)
	return s.clearErr
}

func (s *fakeLoginService) Revalidate(_ context.Context, req authnservice.RevalidateRequest) (*authnentity.LoginAccount, error) {
	s.steps = append(s.steps, "lock")
	if s.lockErr != nil {
		return nil, s.lockErr
	}
	if s.locked == nil {
		s.locked = &authnentity.LoginAccount{ID: req.SystemUserID, UserName: req.UserName}
	}
	return s.locked, nil
}

func (s *fakeLoginService) RecordLogin(_ context.Context, req authnservice.RecordLoginRequest) error {
	s.steps = append(s.steps, "record")
	s.recordIP = req.SourceIP
	s.recordAt = req.LoginTime
	return nil
}

func (s *fakeLoginService) IssueCredential(_ context.Context, req authnservice.IssueCredentialRequest) (*authnentity.LoginResult, error) {
	s.steps = append(s.steps, "issue")
	s.issuedSnapshot = req.Snapshot
	s.issued = req.Session
	if s.issueErr != nil {
		return nil, s.issueErr
	}
	if s.result == nil {
		s.result = &authnentity.LoginResult{AccessToken: "secret"}
	}
	return s.result, nil
}

func (s *fakeLoginService) DiscardCredential(_ context.Context, req authnservice.DiscardCredentialRequest) error {
	s.steps = append(s.steps, "discard")
	s.discarded = append(s.discarded, req.BearerToken)
	return nil
}

func (s *fakeLoginService) Logout(_ context.Context, sessionID string) error {
	s.steps = append(s.steps, "logout")
	s.logoutIDs = append(s.logoutIDs, sessionID)
	return s.logoutErr
}

// fakeAuthorizer stands in for the access crossdomain contract: it records the
// snapshot step and answers from preloaded fields.
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

func newLoginApp(login *fakeLoginService) *LoginApplication {
	return newLoginAppWithTx(login, newFakeTransaction())
}

func newLoginAppWithTx(login *fakeLoginService, tx *fakeTransaction) *LoginApplication {
	app, _ := newLoginAppWithWriter(login, tx)
	return app
}

// newLoginAppWithWriter also hands back the login-log fake so a test can
// assert the recorded rows (ADR 0012).
func newLoginAppWithWriter(login *fakeLoginService, tx *fakeTransaction) (*LoginApplication, *fakeLoginLogWriter) {
	writer := &fakeLoginLogWriter{}
	app := NewLoginApplication(login, &fakeAuthorizer{steps: &login.steps}, writer, tx, testLogger())
	app.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return app, writer
}

// newLoginAppWithAuthorizer lets a test inject its own access contract fake
// (step sharing, resolution content, or failure).
func newLoginAppWithAuthorizer(login *fakeLoginService, authorizer access.AuthorizationResolver) *LoginApplication {
	app := NewLoginApplication(login, authorizer, &fakeLoginLogWriter{}, newFakeTransaction(), testLogger())
	app.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return app
}

func TestIssueLoginChallengeReturnsThePicture(t *testing.T) {
	login := &fakeLoginService{issuedChallenge: &authnentity.LoginChallenge{ID: "cid", Image: []byte{1, 2, 3}}}

	reply, err := newLoginApp(login).IssueLoginChallenge(t.Context(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("IssueLoginChallenge: %v", err)
	}
	if reply.GetChallengeId() != "cid" || string(reply.GetImagePng()) != string([]byte{1, 2, 3}) {
		t.Fatalf("reply = %q/%v", reply.GetChallengeId(), reply.GetImagePng())
	}
	assertSteps(t, &login.steps, "issue-challenge")
}

func TestLoginMapsTheIssuedCredentialOntoTheReply(t *testing.T) {
	login := &fakeLoginService{result: &authnentity.LoginResult{
		AccessToken: "secret",
		ExpiresIn:   7 * 24 * time.Hour,
	}}

	reply, err := newLoginApp(login).Login(t.Context(), &authnv1.LoginRequest{Username: "alice", Password: "correct"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if reply.GetAccessToken() != "secret" {
		t.Fatalf("AccessToken = %q, want the issued token", reply.GetAccessToken())
	}
	if reply.GetExpireIn() != int64((7*24*time.Hour)/time.Second) {
		t.Fatalf("ExpireIn = %d, want %d", reply.GetExpireIn(), int64((7*24*time.Hour)/time.Second))
	}
}

func TestLoginPropagatesVerificationFailure(t *testing.T) {
	login := &fakeLoginService{verifyErr: authndomain.ErrInvalidCredentials}

	_, err := newLoginApp(login).authenticate(t.Context(), &LoginRequest{UserName: "alice", Password: "wrong"})
	if !errors.Is(err, authndomain.ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want the verification failure", err)
	}
	// A failed verification never opens the transaction: nothing was cleared,
	// recorded, or issued.
	assertSteps(t, &login.steps, "challenge", "verify")
}

func TestLoginClearFailureAbortsBeforeTheTransaction(t *testing.T) {
	login := &fakeLoginService{clearErr: authndomain.ErrAuthBackendUnavailable}

	_, err := newLoginApp(login).authenticate(t.Context(), &LoginRequest{UserName: "alice", Password: "correct"})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Login error = %v, want the counter-clearing failure", err)
	}
	assertSteps(t, &login.steps, "challenge", "verify", "clear")
}

func TestLoginSuccessRunsServiceStepsThenIssuesCredentialLast(t *testing.T) {
	login := &fakeLoginService{result: &authnentity.LoginResult{AccessToken: "secret", ExpiresIn: 7 * 24 * time.Hour}}

	got, err := newLoginApp(login).authenticate(t.Context(), &LoginRequest{
		UserName:  " alice ",
		Password:  "correct",
		SourceIP:  "192.0.2.10",
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0 Mobile/15E148 Safari/604.1",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.AccessToken != "secret" {
		t.Fatalf("AccessToken = %q, want secret", got.AccessToken)
	}
	// The trimmed username reaches the service, and the counter is cleared
	// before any transaction step runs.
	if login.verifyName != "alice" {
		t.Fatalf("VerifyPassword name = %q, want the trimmed alice", login.verifyName)
	}
	if len(login.cleared) != 1 || login.cleared[0] != "alice" {
		t.Fatalf("cleared = %v, want [alice]", login.cleared)
	}
	// Orchestration order: lock → snapshot → record (PostgreSQL) → issue
	// (Redis, deliberately last); nothing after it inside the closure.
	assertSteps(t, &login.steps, "challenge", "verify", "clear", "lock", "snapshot", "record", "issue")
	if login.issued == nil {
		t.Fatal("no Login Session was registered")
	}
	if login.issued.DeviceType != "mobile" || login.issued.Browser != "Chrome" || login.issued.OS != "iOS" {
		t.Fatalf("metadata = %q/%q/%q, want mobile/Chrome/iOS", login.issued.DeviceType, login.issued.Browser, login.issued.OS)
	}
	if login.issued.LoginTime != time.Unix(1_700_000_000, 0) || login.issued.IPAddress != "192.0.2.10" || login.issued.SystemUserID != 42 {
		t.Fatalf("session = %#v", login.issued)
	}
	if login.recordIP != "192.0.2.10" || login.recordAt != time.Unix(1_700_000_000, 0) {
		t.Fatalf("recorded login = %q/%v", login.recordIP, login.recordAt)
	}
}

func TestLoginRevalidationFailureFeedsTheCounter(t *testing.T) {
	login := &fakeLoginService{lockErr: authndomain.ErrInvalidCredentials}

	_, err := newLoginApp(login).authenticate(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	if login.failures != 1 {
		t.Fatalf("failures = %d, want 1 (a stale hash counts as a failed attempt)", login.failures)
	}
	assertSteps(t, &login.steps, "challenge", "verify", "clear", "lock", "fail")
}

func TestLoginIssueFailureRollsBackWithoutCompensation(t *testing.T) {
	login := &fakeLoginService{issueErr: authndomain.ErrAuthBackendUnavailable}

	_, err := newLoginApp(login).authenticate(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Login error = %v, want ErrAuthBackendUnavailable", err)
	}
	// The credential was never fully issued, so the service cleaned up its
	// partial writes itself — the usecase must not discard again.
	if len(login.discarded) != 0 {
		t.Fatalf("discarded = %v, want none on issue failure", login.discarded)
	}
	assertSteps(t, &login.steps, "challenge", "verify", "clear", "lock", "snapshot", "record", "issue")
}

func TestLoginCommitFailureDiscardsTheCredential(t *testing.T) {
	login := &fakeLoginService{result: &authnentity.LoginResult{AccessToken: "leaked"}}
	tx := &fakeTransaction{err: authndomain.ErrAuthBackendUnavailable} // commit fails after the body

	_, err := newLoginAppWithTx(login, tx).authenticate(t.Context(), &LoginRequest{
		UserName: "alice",
		Password: "correct",
		SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Login error = %v, want the commit failure", err)
	}
	if len(login.discarded) != 1 || login.discarded[0] != "leaked" {
		t.Fatalf("discarded = %v, want [leaked]", login.discarded)
	}
	assertSteps(t, &login.steps, "challenge", "verify", "clear", "lock", "snapshot", "record", "issue", "discard")
}

func TestLoginResolutionFailureIsAuthBackendUnavailable(t *testing.T) {
	login := &fakeLoginService{}

	_, err := newLoginAppWithAuthorizer(login, &fakeAuthorizer{steps: &login.steps, err: errors.New("access unavailable")}).authenticate(t.Context(), &LoginRequest{
		UserName: "alice", Password: "correct", SourceIP: "192.0.2.10",
	})
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Login error = %v, want ErrAuthBackendUnavailable", err)
	}
	if login.failures != 0 {
		t.Fatalf("failures = %d, want 0 (resolution failures are not login failures)", login.failures)
	}
	// Resolution happens between revalidation and the last-login write; the
	// credential is never issued.
	assertSteps(t, &login.steps, "challenge", "verify", "clear", "lock", "snapshot")
}

func TestLoginFillsSnapshotFromTheAccessContract(t *testing.T) {
	login := &fakeLoginService{result: &authnentity.LoginResult{AccessToken: "secret"}}
	authorizer := &fakeAuthorizer{resolution: &accessmodel.AuthorizationResolution{
		RoleKeys:    []string{"viewer"},
		Permissions: []string{"system:user:list"},
	}}
	app := newLoginAppWithAuthorizer(login, authorizer)

	if _, err := app.authenticate(t.Context(), &LoginRequest{UserName: "alice", Password: "correct"}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	snapshot := login.issuedSnapshot
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
