package iam

import (
	"context"
	"time"
)

// AuthLoginRepo owns the login protocol's storage mechanics. Its database
// methods join the caller's Transaction (each wraps itself in InTx, which is
// nested-safe); IssueCredential performs the Redis writes and must be the
// LAST step of the surrounding InTx closure so a Redis failure rolls the
// PostgreSQL writes back, and DiscardCredential compensates when Redis
// succeeded but the commit failed. sa-token stays behind the data
// implementation; the orchestration order — LockAndRevalidate, LoadSnapshot,
// RecordLogin, IssueCredential — is load-bearing (ADR-0010).
type AuthLoginRepo interface {
	// LockAndRevalidate acquires the System User's authorization advisory lock
	// (held until the surrounding transaction ends) and re-reads the account
	// under it. ErrInvalidCredentials when the user is gone, renamed, or the
	// password hash changed while waiting for the lock; ErrSystemUserDisabled
	// when disabled.
	LockAndRevalidate(ctx context.Context, systemUserID int64, userName, passwordHash string) (*User, error)
	// LoadSnapshot freshly reads the shared Authorization Snapshot (active
	// Role keys, distinct Functional Permissions) for the locked System User,
	// inside the caller's transaction.
	LoadSnapshot(ctx context.Context, systemUserID int64, userName string) (*AuthorizationSnapshot, error)
	// RecordLogin persists the login IP and time on the System User row inside
	// the caller's transaction.
	RecordLogin(ctx context.Context, systemUserID int64, sourceIP string, loginTime time.Time) error
	// IssueCredential mints the bearer credential (the only sa-token
	// Manager.Login call site), replaces the shared Account Session snapshot,
	// and registers the authoritative Login Session. On partial Redis failure
	// it destroys what it wrote before returning the error; a non-nil result
	// means every Redis write succeeded.
	IssueCredential(ctx context.Context, snapshot *AuthorizationSnapshot, session *LoginSession) (*LoginResult, error)
	// DiscardCredential tears down a credential whose transaction failed to
	// commit: destroys the token, its authoritative Login Session, and the
	// Account Session when it was the last one. Best-effort and idempotent.
	DiscardCredential(ctx context.Context, systemUserID int64, bearerToken string) error
}
