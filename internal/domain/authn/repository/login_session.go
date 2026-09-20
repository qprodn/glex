package repository

import (
	"context"
	"time"

	"glex/internal/domain/authn/entity"
)

// LoginAttemptRequest addresses the two failure counters of one login attempt:
// the login identifier and the source IP it arrived from.
type LoginAttemptRequest struct {
	UserName string
	SourceIP string
}

// LoginCounterRepo owns the Redis-backed username and authoritative source-IP
// failure counters. A positive duration means the attempt is locked for at
// least that much longer.
type LoginCounterRepo interface {
	LockRemaining(ctx context.Context, req LoginAttemptRequest) (time.Duration, error)
	RecordFailure(ctx context.Context, req LoginAttemptRequest) (time.Duration, error)
	ClearUserName(ctx context.Context, userName string) error
}

// CreateSessionRequest correlates one newly-issued bearer credential with the
// authoritative Login Session it registers: the credential (req.BearerToken)
// is accepted only at this moment and never read back.
type CreateSessionRequest struct {
	BearerToken string
	Session     *entity.LoginSession
}

// ListBySystemUserRequest lists one System User's Login Sessions: the shared
// list shape (embedded ListOptions — filter, order, page window) scoped to
// req.SystemUserID. Folding the scope in retires the option-family exemption
// ADR 0024 granted this listing (ADR 0025).
type ListBySystemUserRequest struct {
	ListOptions
	SystemUserID int64
}

// SessionOwnershipRequest names one Login Session (req.SessionID) and the
// System User (req.SystemUserID) claiming it — the pair the atomic ownership
// check resolves.
type SessionOwnershipRequest struct {
	SystemUserID int64
	SessionID    string
}

// RevokeSystemUserRequest revokes every Login Session of one System User,
// sparing req.ExceptSessionID when it is non-empty.
type RevokeSystemUserRequest struct {
	SystemUserID    int64
	ExceptSessionID string
}

// RevokeSystemUsersRequest atomically revokes every Login Session of the
// given System Users, sparing req.ExceptSessionID when it is non-empty
// (empty revokes all). The batch takes flat scalars so no other module's
// vocabulary crosses this port; callers classify security changes on their
// own side (ADR 0005).
type RevokeSystemUsersRequest struct {
	SystemUserIDs   []int64
	ExceptSessionID string
}

// LoginSessionRepo is the authoritative project-owned Login Session store.
// It accepts a bearer token only when correlating a newly-issued credential;
// no read method returns that credential.
type LoginSessionRepo interface {
	Create(ctx context.Context, req CreateSessionRequest) (*entity.LoginSession, error)
	FindByID(ctx context.Context, sessionID string) (*entity.LoginSession, error)
	FindByToken(ctx context.Context, bearerToken string) (*entity.LoginSession, error)
	List(ctx context.Context, opts ...ListOption) ([]*entity.LoginSession, error)
	ListBySystemUser(ctx context.Context, req ListBySystemUserRequest) ([]*entity.LoginSession, error)
	Owns(ctx context.Context, req SessionOwnershipRequest) (bool, error)
	Logout(ctx context.Context, sessionID string) error
	LogoutOwned(ctx context.Context, req SessionOwnershipRequest) error
	Kickout(ctx context.Context, sessionID string) error
	Replace(ctx context.Context, sessionID string) error
	RevokeSystemUser(ctx context.Context, req RevokeSystemUserRequest) error
	// RevokeSystemUsers atomically revokes every Login Session of the given
	// System Users except one optional Session ID (empty revokes all). The
	// batch takes flat scalars so no other module's vocabulary crosses this
	// port; callers classify security changes on their own side (ADR 0005).
	RevokeSystemUsers(ctx context.Context, req RevokeSystemUsersRequest) error
}
