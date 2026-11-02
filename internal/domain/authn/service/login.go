// Package service holds the authn module's domain services: the only holders
// of the module's repository ports (ADR 0005). Application use cases call
// these interfaces and never touch a repository directly; the Application
// service remains the transaction root, so every method joins the caller's
// ambient transaction. Credential hashing policy and Principal context
// plumbing remain free functions and stay callable from anywhere.
package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"strings"
	"time"

	"glex/internal/domain/authn"
	"glex/internal/domain/authn/entity"
	"glex/internal/domain/authn/repository"
	"glex/pkg/logx"
)

// ConsumeChallengeRequest spends one Login Challenge: its id and the answer
// the caller read from the picture.
type ConsumeChallengeRequest struct {
	ChallengeID string
	Answer      string
}

// VerifyPasswordRequest is one credential verification attempt: the login
// identifier, the plaintext password, and the source IP the attempt arrived
// from (the two failure counters' subjects).
type VerifyPasswordRequest struct {
	UserName string
	Password string
	SourceIP string
}

// LoginAttemptRequest addresses one login attempt by its two counter
// subjects: the login identifier and the source IP it arrived from.
type LoginAttemptRequest struct {
	UserName string
	SourceIP string
}

// RevalidateRequest re-reads one account inside the caller's transaction: the
// System User row it expects as the caller knew it (id, name, password hash).
type RevalidateRequest struct {
	SystemUserID int64
	UserName     string
	PasswordHash string
}

// RecordLoginRequest persists one sign-in's footprint on the System User row.
type RecordLoginRequest struct {
	SystemUserID int64
	SourceIP     string
	LoginTime    time.Time
}

// IssueCredentialRequest is one credential issuance: the shared Authorization
// Snapshot and the authoritative Login Session registering it.
type IssueCredentialRequest struct {
	Snapshot *entity.AuthorizationSnapshot
	Session  *entity.LoginSession
}

// DiscardCredentialRequest tears down one credential whose transaction failed
// to commit: the System User it belongs to and the bearer token to destroy.
type DiscardCredentialRequest struct {
	SystemUserID int64
	BearerToken  string
}

// Login is the authn domain service behind the Login/Logout endpoints: the
// only path to the login backend, failure counters, Login Challenges, and
// Login Session store (ADR 0005). The Application service owns the transaction
// boundary, the Authorization Snapshot fill through the access crossdomain
// contract, and the commit-failure compensation; these methods are the steps
// it orchestrates. Every error a method returns to its caller first logs its
// Warn breadcrumb (ADR 0011).
type Login interface {
	// IssueChallenge mints one Login Challenge. The answer stays in the store.
	IssueChallenge(ctx context.Context) (*entity.LoginChallenge, error)
	// ConsumeChallenge spends the challenge named by req. A missing, wrong,
	// expired, or already-spent challenge is ErrLoginChallengeInvalid and does
	// not touch the failure counters.
	ConsumeChallenge(ctx context.Context, req ConsumeChallengeRequest) error
	// VerifyPassword gates the attempt on the failure counters, looks up the
	// account without revealing whether it exists, verifies the password, and
	// validates account state — returning the pre-transaction account view on
	// success. Every failure path records on both counters.
	VerifyPassword(ctx context.Context, req VerifyPasswordRequest) (*entity.LoginAccount, error)
	// RecordFailure records one failed attempt on both failure counters and
	// normalizes the outcome (locked reports the remaining lock, otherwise the
	// generic invalid-credentials reason).
	RecordFailure(ctx context.Context, req LoginAttemptRequest) error
	// ClearFailures clears the username counter before a credential is issued.
	ClearFailures(ctx context.Context, userName string) error
	// Revalidate re-reads the account inside the caller's transaction.
	Revalidate(ctx context.Context, req RevalidateRequest) (*entity.LoginAccount, error)
	// RecordLogin persists the login IP and time on the System User row inside
	// the caller's transaction.
	RecordLogin(ctx context.Context, req RecordLoginRequest) error
	// IssueCredential mints the bearer credential, replaces the shared Account
	// Session snapshot, and registers the authoritative Login Session.
	IssueCredential(ctx context.Context, req IssueCredentialRequest) (*entity.LoginResult, error)
	// DiscardCredential tears down a credential whose transaction failed to
	// commit. Best-effort and idempotent.
	DiscardCredential(ctx context.Context, req DiscardCredentialRequest) error
	// Logout revokes one Login Session by its Session ID.
	Logout(ctx context.Context, sessionID string) error
}

type loginImpl struct {
	backend    repository.AuthLoginBackend
	counters   repository.LoginCounterRepo
	sessions   repository.LoginSessionRepo
	challenges repository.LoginChallengeRepo
	logger     *slog.Logger
}

// NewLoginService creates the login domain service. backend reads and writes
// the transactional login account; counters own the Redis failure counters;
// sessions back Logout; challenges store Login Challenges.
func NewLoginService(backend repository.AuthLoginBackend, counters repository.LoginCounterRepo, sessions repository.LoginSessionRepo, challenges repository.LoginChallengeRepo, logger *slog.Logger) Login {
	return &loginImpl{
		backend:    backend,
		counters:   counters,
		sessions:   sessions,
		challenges: challenges,
		logger:     logger.With(slog.String("component", "domain.authn.svc")),
	}
}

func (s *loginImpl) IssueChallenge(ctx context.Context) (*entity.LoginChallenge, error) {
	issued, err := s.challenges.Issue(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.issue_challenge", err)
		return nil, err
	}
	return issued, nil
}

func (s *loginImpl) ConsumeChallenge(ctx context.Context, req ConsumeChallengeRequest) error {
	id := strings.TrimSpace(req.ChallengeID)
	if id == "" {
		err := authn.ErrLoginChallengeInvalid
		logx.Propagate(ctx, s.logger, "authn.svc.consume_challenge", err)
		return err
	}
	stored, err := s.challenges.Consume(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.consume_challenge", err)
		return err
	}
	if !challengeAnswersMatch(stored, req.Answer) {
		err := authn.ErrLoginChallengeInvalid
		logx.Propagate(ctx, s.logger, "authn.svc.consume_challenge", err)
		return err
	}
	return nil
}

// challengeAnswersMatch compares a stored answer with what the caller typed.
// Comparison is case-insensitive. A length mismatch is simply a miss; the
// challenge is already spent by the time this runs.
func challengeAnswersMatch(stored, presented string) bool {
	want := normalizeChallengeAnswer(stored)
	got := normalizeChallengeAnswer(presented)
	if len(want) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func normalizeChallengeAnswer(answer string) string {
	return strings.ToUpper(strings.TrimSpace(answer))
}

func (s *loginImpl) VerifyPassword(ctx context.Context, req VerifyPasswordRequest) (*entity.LoginAccount, error) {
	attempt := LoginAttemptRequest{UserName: req.UserName, SourceIP: req.SourceIP}
	if remaining, err := s.counters.LockRemaining(ctx, repository.LoginAttemptRequest{UserName: attempt.UserName, SourceIP: attempt.SourceIP}); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.verify_password", err)
		return nil, err
	} else if remaining > 0 {
		err := authn.NewLoginLocked(remaining)
		logx.Propagate(ctx, s.logger, "authn.svc.verify_password", err)
		return nil, err
	}
	account, err := s.backend.FindLoginAccount(ctx, req.UserName)
	if err != nil {
		if !errors.Is(err, authn.ErrLoginAccountNotFound) && !errors.Is(err, authn.ErrLoginAccountAmbiguous) {
			logx.Propagate(ctx, s.logger, "authn.svc.verify_password", err)
			return nil, err
		}
		// Timing decoy, not an ignored error: verifyPassword returns a bool,
		// and only its bcrypt cost matters here — the match result carries no
		// information (ADR 0007 `_ =` audit).
		_ = verifyPassword(DecoyCredentialHash, req.Password)
		return nil, s.RecordFailure(ctx, attempt)
	}
	if !verifyPassword(account.PasswordHash, req.Password) {
		return nil, s.RecordFailure(ctx, attempt)
	}
	if account.Status != 0 {
		err := authn.ErrSystemUserDisabled
		logx.Propagate(ctx, s.logger, "authn.svc.verify_password", err)
		return nil, err
	}
	return account, nil
}

func (s *loginImpl) RecordFailure(ctx context.Context, req LoginAttemptRequest) error {
	if err := recordInvalidLogin(ctx, s.counters, req.UserName, req.SourceIP); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.record_failure", err)
		return err
	}
	return nil
}

func (s *loginImpl) ClearFailures(ctx context.Context, userName string) error {
	if err := s.counters.ClearUserName(ctx, userName); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.clear_failures", err)
		return err
	}
	return nil
}

func (s *loginImpl) Revalidate(ctx context.Context, req RevalidateRequest) (*entity.LoginAccount, error) {
	account, err := s.backend.Revalidate(ctx, repository.RevalidateRequest{
		SystemUserID: req.SystemUserID,
		UserName:     req.UserName,
		PasswordHash: req.PasswordHash,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.revalidate", err)
		return nil, err
	}
	return account, nil
}

func (s *loginImpl) RecordLogin(ctx context.Context, req RecordLoginRequest) error {
	if err := s.backend.RecordLogin(ctx, repository.RecordLoginRequest{
		SystemUserID: req.SystemUserID,
		SourceIP:     req.SourceIP,
		LoginTime:    req.LoginTime,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.record_login", err)
		return err
	}
	return nil
}

func (s *loginImpl) IssueCredential(ctx context.Context, req IssueCredentialRequest) (*entity.LoginResult, error) {
	result, err := s.backend.IssueCredential(ctx, repository.IssueCredentialRequest{
		Snapshot: req.Snapshot,
		Session:  req.Session,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.issue_credential", err)
		return nil, err
	}
	return result, nil
}

func (s *loginImpl) DiscardCredential(ctx context.Context, req DiscardCredentialRequest) error {
	if err := s.backend.DiscardCredential(ctx, repository.DiscardCredentialRequest{
		SystemUserID: req.SystemUserID,
		BearerToken:  req.BearerToken,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.discard_credential", err)
		return err
	}
	return nil
}

func (s *loginImpl) Logout(ctx context.Context, sessionID string) error {
	if err := s.sessions.Logout(ctx, sessionID); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.logout", err)
		return err
	}
	return nil
}

// recordInvalidLogin records one failed attempt on both failure counters and
// normalizes the outcome: a locked counter reports the lock (with its
// remaining duration), otherwise the generic invalid-credentials reason. The
// caller deliberately cannot distinguish which counter locked. Its caller's
// method boundary owns the breadcrumb (ADR 0011). Package-internal by design
// (ADR 0025): it reaches for a repo, so it leaves the exported surface
// instead of growing a request struct.
func recordInvalidLogin(ctx context.Context, counterRepo repository.LoginCounterRepo, userName, sourceIP string) error {
	remaining, err := counterRepo.RecordFailure(ctx, repository.LoginAttemptRequest{UserName: userName, SourceIP: sourceIP})
	if err != nil {
		return err
	}
	if remaining > 0 {
		return authn.NewLoginLocked(remaining)
	}
	return authn.ErrInvalidCredentials
}
