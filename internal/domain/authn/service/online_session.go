package service

import (
	"context"
	"log/slog"

	"glex/internal/domain/authn/entity"
	"glex/internal/domain/authn/repository"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// OnlineSession is the authn domain service behind the Online Session
// (在线会话) endpoints and the crossdomain revocation contract: the only path
// to the Login Session store's monitoring and revocation reads and writes
// (ADR 0005). Every error a method returns to its caller first logs its Warn
// breadcrumb (ADR 0011).
type OnlineSession interface {
	// Get returns one Login Session by its non-secret Session ID — the
	// metadata read behind the revocation endpoints' login-log records
	// (ADR 0012).
	Get(ctx context.Context, sessionID string) (*entity.LoginSession, error)
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
	logger   *slog.Logger
}

// NewOnlineSessionService creates the Online Session domain service.
func NewOnlineSessionService(sessions repository.LoginSessionRepo, logger *slog.Logger) OnlineSession {
	return &onlineSessionImpl{
		sessions: sessions,
		logger:   logger.With(slog.String("component", "domain.authn.svc")),
	}
}

// Get returns one Login Session by Session ID. A missing session is an
// expected business condition — the caller decides whether it is an error.
func (s *onlineSessionImpl) Get(ctx context.Context, sessionID string) (*entity.LoginSession, error) {
	session, err := s.sessions.FindByID(ctx, sessionID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.get_online_session", err)
		return nil, err
	}
	return session, nil
}

func (s *onlineSessionImpl) List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.LoginSession, error) {
	sessions, err := s.sessions.List(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
		repository.ListLimit(limit),
		repository.ListOffset(offset),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.list_online_sessions", err)
		return nil, err
	}
	return sessions, nil
}

func (s *onlineSessionImpl) ListBySystemUser(ctx context.Context, systemUserID int64) ([]*entity.LoginSession, error) {
	sessions, err := s.sessions.ListBySystemUser(ctx, systemUserID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.list_by_system_user", err)
		return nil, err
	}
	return sessions, nil
}

func (s *onlineSessionImpl) Kickout(ctx context.Context, sessionID string) error {
	if err := s.sessions.Kickout(ctx, sessionID); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.kickout", err)
		return err
	}
	return nil
}

func (s *onlineSessionImpl) LogoutOwned(ctx context.Context, systemUserID int64, sessionID string) error {
	if err := s.sessions.LogoutOwned(ctx, systemUserID, sessionID); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.logout_owned", err)
		return err
	}
	return nil
}

func (s *onlineSessionImpl) RevokeSystemUsers(ctx context.Context, systemUserIDs []int64, exceptSessionID string) error {
	if err := s.sessions.RevokeSystemUsers(ctx, systemUserIDs, exceptSessionID); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.revoke_system_users", err)
		return err
	}
	return nil
}
