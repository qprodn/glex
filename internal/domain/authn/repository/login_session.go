package repository

import (
	"context"
	"time"

	"glex/internal/domain/authn/entity"
)

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
	Create(ctx context.Context, bearerToken string, session *entity.LoginSession) (*entity.LoginSession, error)
	FindByID(ctx context.Context, sessionID string) (*entity.LoginSession, error)
	FindByToken(ctx context.Context, bearerToken string) (*entity.LoginSession, error)
	List(ctx context.Context, opts ...ListOption) ([]*entity.LoginSession, error)
	ListBySystemUser(ctx context.Context, systemUserID int64, opts ...ListOption) ([]*entity.LoginSession, error)
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
