// Package repository holds the authn module's ports: the login backend,
// credential, counter, and session repository interfaces, plus the
// ListOption family.
package repository

import (
	"context"
	"time"

	"glex/internal/domain/authn/entity"
)

// LoginAccountReader reads the pre-transaction account view login needs to
// verify credentials. ErrLoginAccountNotFound and ErrLoginAccountAmbiguous are
// mapped by the login Application service onto the generic
// invalid-credentials path.
type LoginAccountReader interface {
	// FindLoginAccount loads the account for a login identifier. The two
	// sentinel errors mean "cannot authenticate as this identifier"; every
	// other error is a backend failure and propagates.
	FindLoginAccount(ctx context.Context, userName string) (*entity.LoginAccount, error)
}

// RevalidateRequest re-reads one account inside the caller's transaction: the
// System User row it expects (req.SystemUserID) as it knew it
// (req.UserName, req.PasswordHash), so a rename, rehash, or withdrawal fails
// the revalidation instead of issuing a credential.
type RevalidateRequest struct {
	SystemUserID int64
	UserName     string
	PasswordHash string
}

// RecordLoginRequest persists one sign-in's footprint: the login IP and time
// written on the System User row req.SystemUserID.
type RecordLoginRequest struct {
	SystemUserID int64
	SourceIP     string
	LoginTime    time.Time
}

// LoginAccountStore owns the transactional database side of login. Its
// methods use the Application transaction propagated through ctx.
type LoginAccountStore interface {
	// Revalidate re-reads the account inside the caller's transaction.
	// ErrInvalidCredentials means the user is gone, renamed, or its password
	// hash changed; ErrSystemUserDisabled means it is disabled.
	Revalidate(ctx context.Context, req RevalidateRequest) (*entity.LoginAccount, error)
	// RecordLogin persists the login IP and time on the System User row inside
	// the caller's transaction.
	RecordLogin(ctx context.Context, req RecordLoginRequest) error
}

// IssueCredentialRequest is one credential issuance: the shared Authorization
// Snapshot (req.Snapshot) every Login Session of the System User reads, and
// the authoritative Login Session registering it (req.Session).
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

// CredentialIssuer owns bearer credential and Login Session writes. Issue is
// the final step of the login transaction closure; Discard compensates when
// those writes succeed but the database commit fails.
type CredentialIssuer interface {
	// IssueCredential mints the bearer credential (the only sa-token
	// Manager.Login call site), replaces the shared Account Session snapshot,
	// and registers the authoritative Login Session. On partial Redis failure
	// it destroys what it wrote before returning the error; a non-nil result
	// means every Redis write succeeded.
	IssueCredential(ctx context.Context, req IssueCredentialRequest) (*entity.LoginResult, error)
	// DiscardCredential tears down a credential whose transaction failed to
	// commit: destroys the token, its authoritative Login Session, and the
	// Account Session when it was the last one. Best-effort and idempotent.
	DiscardCredential(ctx context.Context, req DiscardCredentialRequest) error
}

// AuthLoginBackend is the narrow capability set required by the login
// Application service.
type AuthLoginBackend interface {
	LoginAccountReader
	LoginAccountStore
	CredentialIssuer
}
