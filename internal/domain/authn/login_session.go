package authn

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
)

// LoginSession is one revocable sign-in by a System User. ID is the public,
// non-secret Session ID; the bearer token deliberately is not part of this DO.
type LoginSession struct {
	ID           string
	SystemUserID int64
	UserName     string
	DeviceType   string
	IPAddress    string
	Browser      string
	OS           string
	LoginTime    time.Time
}

// AuthorizationSnapshot is the shared account-level view used by every Login
// Session of one System User. It contains active Role keys and distinct
// Functional Permission handles only; route-tree presentation is deliberately
// not part of this snapshot.
type AuthorizationSnapshot struct {
	SystemUserID int64
	UserName     string
	RoleKeys     []string
	Permissions  []string
}

// LoginResult returns the bearer credential exactly once after the shared
// Authorization Snapshot and authoritative Login Session both exist.
type LoginResult struct {
	AccessToken string
	ExpiresIn   time.Duration
}

// LoginCounterRepo owns the Redis-backed username and authoritative source-IP
// failure counters. A positive duration means the attempt is locked for at
// least that much longer.
type LoginCounterRepo interface {
	LockRemaining(ctx context.Context, userName, sourceIP string) (time.Duration, error)
	RecordFailure(ctx context.Context, userName, sourceIP string) (time.Duration, error)
	ClearUserName(ctx context.Context, userName string) error
}

// LoginSessionRepo is the authoritative project-owned Login Session store.
// It accepts a bearer token only when correlating a newly-issued credential;
// no read method returns that credential.
type LoginSessionRepo interface {
	Create(ctx context.Context, bearerToken string, session *LoginSession) (*LoginSession, error)
	FindByID(ctx context.Context, sessionID string) (*LoginSession, error)
	FindByToken(ctx context.Context, bearerToken string) (*LoginSession, error)
	List(ctx context.Context, opts ...ListOption) ([]*LoginSession, error)
	ListBySystemUser(ctx context.Context, systemUserID int64, opts ...ListOption) ([]*LoginSession, error)
	Owns(ctx context.Context, systemUserID int64, sessionID string) (bool, error)
	Logout(ctx context.Context, sessionID string) error
	LogoutOwned(ctx context.Context, systemUserID int64, sessionID string) error
	Kickout(ctx context.Context, sessionID string) error
	Replace(ctx context.Context, sessionID string) error
	RevokeSystemUser(ctx context.Context, systemUserID int64, exceptSessionID string) error
	// RevokeSystemUsers atomically revokes every Login Session of the given
	// System Users except one optional Session ID (empty revokes all). The
	// batch takes flat scalars so no other module's vocabulary crosses this
	// port; callers classify security changes on their own side (ADR 0016).
	RevokeSystemUsers(ctx context.Context, systemUserIDs []int64, exceptSessionID string) error
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
