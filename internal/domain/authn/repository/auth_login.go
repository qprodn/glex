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

// LoginAccountStore owns the transactional database side of login. Its
// methods use the Application transaction propagated through ctx.
type LoginAccountStore interface {
	// Revalidate re-reads the account inside the caller's transaction.
	// ErrInvalidCredentials means the user is gone, renamed, or its password
	// hash changed; ErrSystemUserDisabled means it is disabled.
	Revalidate(ctx context.Context, systemUserID int64, userName, passwordHash string) (*entity.LoginAccount, error)
	// RecordLogin persists the login IP and time on the System User row inside
	// the caller's transaction.
	RecordLogin(ctx context.Context, systemUserID int64, sourceIP string, loginTime time.Time) error
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
	IssueCredential(ctx context.Context, snapshot *entity.AuthorizationSnapshot, session *entity.LoginSession) (*entity.LoginResult, error)
	// DiscardCredential tears down a credential whose transaction failed to
	// commit: destroys the token, its authoritative Login Session, and the
	// Account Session when it was the last one. Best-effort and idempotent.
	DiscardCredential(ctx context.Context, systemUserID int64, bearerToken string) error
}

// AuthLoginBackend is the narrow capability set required by the login
// Application service.
type AuthLoginBackend interface {
	LoginAccountReader
	LoginAccountStore
	CredentialIssuer
}
