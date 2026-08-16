package biz

import (
	"context"
	"time"
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

// LoginRequest contains credentials plus the authoritative request metadata
// needed to create a Login Session. Password is plaintext only while the login
// usecase verifies it and is never persisted or returned.
type LoginRequest struct {
	UserName  string
	Password  string
	SourceIP  string
	UserAgent string
}

// LoginResult returns the bearer credential exactly once after the shared
// Authorization Snapshot and authoritative Login Session both exist.
type LoginResult struct {
	AccessToken string
	ExpiresIn   time.Duration
}

// LoginIssue is the already-verified identity and safe Login Session metadata
// passed from biz to the data wrapper. PasswordHash lets the wrapper prove the
// credential did not change while it waited for the PostgreSQL advisory lock.
type LoginIssue struct {
	SystemUserID int64
	UserName     string
	PasswordHash string
	SourceIP     string
	DeviceType   string
	Browser      string
	OS           string
	LoginTime    time.Time
}

// AuthLoginRepo is the data-layer login wrapper. Its implementation is the
// only project call site allowed to invoke sa-token Manager.Login; it holds the
// authorization lock, rebuilds the shared snapshot, and registers the Login
// Session before returning a bearer credential.
type AuthLoginRepo interface {
	Issue(ctx context.Context, issue *LoginIssue) (*LoginResult, error)
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
}
