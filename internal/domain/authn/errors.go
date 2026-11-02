// Package authn owns the authentication module per ADR 0005: Credentials
// (hash, policy, verification), Login Sessions and their tokens, and the
// Principal. The credential column physically stays on the System User table,
// but only authn's adapters read or write it. The package root holds only the
// typed errors; the objects live in entity, the ports in repository, and the
// module's own rules in service.
package authn

import (
	"strconv"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
)

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
	// ErrLoginChallengeInvalid: the Login Challenge was missing, wrong,
	// expired, or already spent. The Credential was not checked.
	ErrLoginChallengeInvalid = errors.Unauthorized("LOGIN_CHALLENGE_INVALID", "login challenge is invalid")
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

var (
	// ErrLoginSessionNotFound: the Session ID is absent or expired.
	ErrLoginSessionNotFound = errors.NotFound("TOKEN_INVALID", "Login Session not found")
	// ErrLoginSessionNotOwned: a System User attempted to remove another
	// System User's Login Session.
	ErrLoginSessionNotOwned = errors.Forbidden("PERMISSION_DENIED", "Login Session belongs to another System User")
	// Authentication-state errors are mapped by data so no sa-token or Redis
	// error crosses the repository boundary.
	ErrTokenInvalid           = errors.Unauthorized("TOKEN_INVALID", "invalid token")
	ErrTokenExpired           = errors.Unauthorized("TOKEN_EXPIRED", "token expired")
	ErrTokenIdleTimeout       = errors.Unauthorized("TOKEN_IDLE_TIMEOUT", "Login Session idle timeout")
	ErrLoginSessionKickedOut  = errors.Unauthorized("SESSION_KICKED_OUT", "Login Session kicked out")
	ErrLoginSessionReplaced   = errors.Unauthorized("SESSION_REPLACED", "Login Session replaced")
	ErrAuthBackendUnavailable = errors.ServiceUnavailable("AUTH_BACKEND_UNAVAILABLE", "authentication backend unavailable")
)

var (
	// ErrCredentialMissing: no authenticated Principal is present in the
	// context.
	ErrCredentialMissing = errors.Unauthorized("CREDENTIAL_MISSING", "Authorization credential is required")
	// ErrCredentialMalformed: the Authorization header does not carry a single
	// bearer credential. Raised by the HTTP middleware's own mirror of this
	// error.
	ErrCredentialMalformed = errors.Unauthorized("CREDENTIAL_MALFORMED", "Authorization credential is malformed")
	// ErrPermissionDenied: the Principal lacks the Functional Permission an
	// RPC requires. Raised by the HTTP middleware's own mirror of this error.
	ErrPermissionDenied = errors.Forbidden("PERMISSION_DENIED", "Functional Permission denied")
)
