package service

import (
	"context"

	"glex/internal/domain/authn/entity"
	"glex/internal/domain/authn/repository"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// OnlineSession is the authn domain service behind the Online Session
// (在线会话) endpoints and the crossdomain revocation contract: the only path
// to the Login Session store's monitoring and revocation reads and writes
// (ADR 0005).
type OnlineSession interface {
	List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.LoginSession, error)
	ListBySystemUser(ctx context.Context, systemUserID int64) ([]*entity.LoginSession, error)
	// Kickout force-revokes one Session ID for an administrator.
	Kickout(ctx context.Context, sessionID string) error
	// LogoutOwned revokes one Session after an atomic ownership check against
	// the System User.
	LogoutOwned(ctx context.Context, systemUserID int64, sessionID string) error
	// RevokeSystemUsers atomically revokes every Login Session of the given
	// System Users except one optional Session ID (empty revokes all).
	RevokeSystemUsers(ctx context.Context, systemUserIDs []int64, exceptSessionID string) error
}

type onlineSessionImpl struct {
	sessions repository.LoginSessionRepo
}

// NewOnlineSessionService creates the Online Session domain service.
func NewOnlineSessionService(sessions repository.LoginSessionRepo) OnlineSession {
	return &onlineSessionImpl{sessions: sessions}
}

func (s *onlineSessionImpl) List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.LoginSession, error) {
	return s.sessions.List(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
		repository.ListLimit(limit),
		repository.ListOffset(offset),
	)
}

func (s *onlineSessionImpl) ListBySystemUser(ctx context.Context, systemUserID int64) ([]*entity.LoginSession, error) {
	return s.sessions.ListBySystemUser(ctx, systemUserID)
}

func (s *onlineSessionImpl) Kickout(ctx context.Context, sessionID string) error {
	return s.sessions.Kickout(ctx, sessionID)
}

func (s *onlineSessionImpl) LogoutOwned(ctx context.Context, systemUserID int64, sessionID string) error {
	return s.sessions.LogoutOwned(ctx, systemUserID, sessionID)
}

func (s *onlineSessionImpl) RevokeSystemUsers(ctx context.Context, systemUserIDs []int64, exceptSessionID string) error {
	return s.sessions.RevokeSystemUsers(ctx, systemUserIDs, exceptSessionID)
}
