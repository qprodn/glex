// Package service holds the authn module's domain services: the only holders
// of the module's repository ports (ADR 0005). Application use cases call
// these interfaces and never touch a repository directly; the Application
// service remains the transaction root, so every method joins the caller's
// ambient transaction. Credential hashing policy and Principal context
// plumbing remain free functions and stay callable from anywhere.
package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"glex/internal/domain/authn"
	"glex/internal/domain/authn/entity"
	"glex/internal/domain/authn/repository"
	"glex/pkg/logx"
)

// Login is the authn domain service behind the Login/Logout endpoints: the
// only path to the login backend, failure counters, and Login Session store
// (ADR 0005). The Application service owns the transaction boundary, the
// Authorization Snapshot fill through the access crossdomain contract, and
// the commit-failure compensation; these methods are the steps it orchestrates.
// Every error a method returns to its caller first logs its Warn breadcrumb
// (ADR 0011).
type Login interface {
	// VerifyPassword gates the attempt on the failure counters, looks up the
	// account without revealing whether it exists, verifies the password, and
	// validates account state — returning the pre-transaction account view on
	// success. Every failure path records on both counters.
	VerifyPassword(ctx context.Context, userName, password, sourceIP string) (*entity.LoginAccount, error)
	// RecordFailure records one failed attempt on both failure counters and
	// normalizes the outcome (locked reports the remaining lock, otherwise the
	// generic invalid-credentials reason).
	RecordFailure(ctx context.Context, userName, sourceIP string) error
	// ClearFailures clears the username counter before a credential is issued.
	ClearFailures(ctx context.Context, userName string) error
	// Revalidate re-reads the account inside the caller's transaction.
	Revalidate(ctx context.Context, systemUserID int64, userName, passwordHash string) (*entity.LoginAccount, error)
	// RecordLogin persists the login IP and time on the System User row inside
	// the caller's transaction.
	RecordLogin(ctx context.Context, systemUserID int64, sourceIP string, loginTime time.Time) error
	// IssueCredential mints the bearer credential, replaces the shared Account
	// Session snapshot, and registers the authoritative Login Session.
	IssueCredential(ctx context.Context, snapshot *entity.AuthorizationSnapshot, session *entity.LoginSession) (*entity.LoginResult, error)
	// DiscardCredential tears down a credential whose transaction failed to
	// commit. Best-effort and idempotent.
	DiscardCredential(ctx context.Context, systemUserID int64, bearerToken string) error
	// Logout revokes one Login Session by its Session ID.
	Logout(ctx context.Context, sessionID string) error
}

type loginImpl struct {
	backend  repository.AuthLoginBackend
	counters repository.LoginCounterRepo
	sessions repository.LoginSessionRepo
	logger   *slog.Logger
}

// NewLoginService creates the login domain service. backend reads and writes
// the transactional login account; counters own the Redis failure counters;
// sessions back Logout.
func NewLoginService(backend repository.AuthLoginBackend, counters repository.LoginCounterRepo, sessions repository.LoginSessionRepo, logger *slog.Logger) Login {
	return &loginImpl{
		backend:  backend,
		counters: counters,
		sessions: sessions,
		logger:   logger.With(slog.String("component", "domain.authn.svc")),
	}
}

func (s *loginImpl) VerifyPassword(ctx context.Context, userName, password, sourceIP string) (*entity.LoginAccount, error) {
	if remaining, err := s.counters.LockRemaining(ctx, userName, sourceIP); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.verify_password", err)
		return nil, err
	} else if remaining > 0 {
		err := authn.NewLoginLocked(remaining)
		logx.Propagate(ctx, s.logger, "authn.svc.verify_password", err)
		return nil, err
	}
	account, err := s.backend.FindLoginAccount(ctx, userName)
	if err != nil {
		if !errors.Is(err, authn.ErrLoginAccountNotFound) && !errors.Is(err, authn.ErrLoginAccountAmbiguous) {
			logx.Propagate(ctx, s.logger, "authn.svc.verify_password", err)
			return nil, err
		}
		// Timing decoy, not an ignored error: VerifyPassword returns a bool,
		// and only its bcrypt cost matters here — the match result carries no
		// information (ADR 0007 `_ =` audit).
		_ = VerifyPassword(DecoyCredentialHash, password)
		return nil, s.RecordFailure(ctx, userName, sourceIP)
	}
	if !VerifyPassword(account.PasswordHash, password) {
		return nil, s.RecordFailure(ctx, userName, sourceIP)
	}
	if account.Status != 0 {
		err := authn.ErrSystemUserDisabled
		logx.Propagate(ctx, s.logger, "authn.svc.verify_password", err)
		return nil, err
	}
	return account, nil
}

func (s *loginImpl) RecordFailure(ctx context.Context, userName, sourceIP string) error {
	if err := RecordInvalidLogin(ctx, s.counters, userName, sourceIP); err != nil {
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

func (s *loginImpl) Revalidate(ctx context.Context, systemUserID int64, userName, passwordHash string) (*entity.LoginAccount, error) {
	account, err := s.backend.Revalidate(ctx, systemUserID, userName, passwordHash)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.revalidate", err)
		return nil, err
	}
	return account, nil
}

func (s *loginImpl) RecordLogin(ctx context.Context, systemUserID int64, sourceIP string, loginTime time.Time) error {
	if err := s.backend.RecordLogin(ctx, systemUserID, sourceIP, loginTime); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.record_login", err)
		return err
	}
	return nil
}

func (s *loginImpl) IssueCredential(ctx context.Context, snapshot *entity.AuthorizationSnapshot, session *entity.LoginSession) (*entity.LoginResult, error) {
	result, err := s.backend.IssueCredential(ctx, snapshot, session)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.issue_credential", err)
		return nil, err
	}
	return result, nil
}

func (s *loginImpl) DiscardCredential(ctx context.Context, systemUserID int64, bearerToken string) error {
	if err := s.backend.DiscardCredential(ctx, systemUserID, bearerToken); err != nil {
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

// RecordInvalidLogin records one failed attempt on both failure counters and
// normalizes the outcome: a locked counter reports the lock (with its
// remaining duration), otherwise the generic invalid-credentials reason. The
// caller deliberately cannot distinguish which counter locked. Its caller's
// method boundary owns the breadcrumb (ADR 0011).
func RecordInvalidLogin(ctx context.Context, counterRepo repository.LoginCounterRepo, userName, sourceIP string) error {
	remaining, err := counterRepo.RecordFailure(ctx, userName, sourceIP)
	if err != nil {
		return err
	}
	if remaining > 0 {
		return authn.NewLoginLocked(remaining)
	}
	return authn.ErrInvalidCredentials
}
