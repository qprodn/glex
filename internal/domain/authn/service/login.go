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
	"time"

	"glex/internal/domain/authn"
	"glex/internal/domain/authn/entity"
	"glex/internal/domain/authn/repository"
)

// Login is the authn domain service behind the Login/Logout endpoints: the
// only path to the login backend, failure counters, and Login Session store
// (ADR 0005). The Application service owns the transaction boundary, the
// Authorization Snapshot fill through the access crossdomain contract, and
// the commit-failure compensation; these methods are the steps it orchestrates.
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
}

// NewLoginService creates the login domain service. backend reads and writes
// the transactional login account; counters own the Redis failure counters;
// sessions back Logout.
func NewLoginService(backend repository.AuthLoginBackend, counters repository.LoginCounterRepo, sessions repository.LoginSessionRepo) Login {
	return &loginImpl{backend: backend, counters: counters, sessions: sessions}
}

func (s *loginImpl) VerifyPassword(ctx context.Context, userName, password, sourceIP string) (*entity.LoginAccount, error) {
	if remaining, err := s.counters.LockRemaining(ctx, userName, sourceIP); err != nil {
		return nil, err
	} else if remaining > 0 {
		return nil, authn.NewLoginLocked(remaining)
	}
	account, err := s.backend.FindLoginAccount(ctx, userName)
	if err != nil {
		if !errors.Is(err, authn.ErrLoginAccountNotFound) && !errors.Is(err, authn.ErrLoginAccountAmbiguous) {
			return nil, err
		}
		_ = VerifyPassword(DecoyCredentialHash, password)
		return nil, s.RecordFailure(ctx, userName, sourceIP)
	}
	if !VerifyPassword(account.PasswordHash, password) {
		return nil, s.RecordFailure(ctx, userName, sourceIP)
	}
	if account.Status != 0 {
		return nil, authn.ErrSystemUserDisabled
	}
	return account, nil
}

func (s *loginImpl) RecordFailure(ctx context.Context, userName, sourceIP string) error {
	return RecordInvalidLogin(ctx, s.counters, userName, sourceIP)
}

func (s *loginImpl) ClearFailures(ctx context.Context, userName string) error {
	return s.counters.ClearUserName(ctx, userName)
}

func (s *loginImpl) Revalidate(ctx context.Context, systemUserID int64, userName, passwordHash string) (*entity.LoginAccount, error) {
	return s.backend.Revalidate(ctx, systemUserID, userName, passwordHash)
}

func (s *loginImpl) RecordLogin(ctx context.Context, systemUserID int64, sourceIP string, loginTime time.Time) error {
	return s.backend.RecordLogin(ctx, systemUserID, sourceIP, loginTime)
}

func (s *loginImpl) IssueCredential(ctx context.Context, snapshot *entity.AuthorizationSnapshot, session *entity.LoginSession) (*entity.LoginResult, error) {
	return s.backend.IssueCredential(ctx, snapshot, session)
}

func (s *loginImpl) DiscardCredential(ctx context.Context, systemUserID int64, bearerToken string) error {
	return s.backend.DiscardCredential(ctx, systemUserID, bearerToken)
}

func (s *loginImpl) Logout(ctx context.Context, sessionID string) error {
	return s.sessions.Logout(ctx, sessionID)
}

// RecordInvalidLogin records one failed attempt on both failure counters and
// normalizes the outcome: a locked counter reports the lock (with its
// remaining duration), otherwise the generic invalid-credentials reason. The
// caller deliberately cannot distinguish which counter locked.
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
