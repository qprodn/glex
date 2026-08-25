package authn

import (
	"context"
	"strconv"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
)

// LoginAccount is the authentication view of a System User account: the login
// identifier, the stored credential hash, and the status gate. It is the only
// shape through which login reads the credential column (ADR 0016); profile
// fields stay with the user module.
type LoginAccount struct {
	ID           int64
	UserName     string
	PasswordHash string
	Status       int32
}

// LoginAccountReader reads the pre-transaction account view login needs to
// verify credentials. ErrLoginAccountNotFound and ErrLoginAccountAmbiguous are
// mapped by the login Usecase onto the generic invalid-credentials path.
type LoginAccountReader interface {
	// FindLoginAccount loads the account for a login identifier. The two
	// sentinel errors mean "cannot authenticate as this identifier"; every
	// other error is a backend failure and propagates.
	FindLoginAccount(ctx context.Context, userName string) (*LoginAccount, error)
}

// LoginAccountStore owns the transactional database side of login. Its
// methods use the Application transaction propagated through ctx.
type LoginAccountStore interface {
	// Revalidate re-reads the account inside the caller's transaction.
	// ErrInvalidCredentials means the user is gone, renamed, or its password
	// hash changed; ErrSystemUserDisabled means it is disabled.
	Revalidate(ctx context.Context, systemUserID int64, userName, passwordHash string) (*LoginAccount, error)
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
	IssueCredential(ctx context.Context, snapshot *AuthorizationSnapshot, session *LoginSession) (*LoginResult, error)
	// DiscardCredential tears down a credential whose transaction failed to
	// commit: destroys the token, its authoritative Login Session, and the
	// Account Session when it was the last one. Best-effort and idempotent.
	DiscardCredential(ctx context.Context, systemUserID int64, bearerToken string) error
}

// AuthLoginBackend is the narrow capability set required by the login
// Application Usecase.
type AuthLoginBackend interface {
	LoginAccountReader
	LoginAccountStore
	CredentialIssuer
}

var (
	// ErrInvalidCredentials: the presented password is wrong (or the System
	// User is gone, renamed, or its password hash changed).
	ErrInvalidCredentials = errors.Unauthorized("INVALID_CREDENTIALS", "invalid username or password")
	// ErrSystemUserDisabled: the System User exists but is disabled.
	ErrSystemUserDisabled = errors.Forbidden("ACCOUNT_DISABLED", "System User is disabled")
	// ErrLoginAccountNotFound: no active account answers the login identifier.
	// Login maps it onto the generic invalid-credentials path.
	ErrLoginAccountNotFound = errors.Unauthorized("LOGIN_ACCOUNT_NOT_FOUND", "login account not found")
	// ErrLoginAccountAmbiguous: a best-effort uniqueness race left more than
	// one active account answering the login identifier. Login maps it onto
	// the generic invalid-credentials path.
	ErrLoginAccountAmbiguous = errors.Conflict("LOGIN_ACCOUNT_AMBIGUOUS", "login account is ambiguous")
)

// NewLoginLocked returns the stable LOGIN_LOCKED reason together with a
// rounded-up remaining duration. The metadata does not identify which counter
// (username or source IP) caused the lock.
func NewLoginLocked(remaining time.Duration) error {
	seconds := int64((remaining + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return errors.Unauthorized("LOGIN_LOCKED", "login temporarily locked").
		WithMetadata(map[string]string{"remaining_seconds": strconv.FormatInt(seconds, 10)})
}
