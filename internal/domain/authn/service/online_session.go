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

// ListSessionsRequest is one Online Session listing: the raw AIP-160 filter,
// AIP-132 order, and page window. No listq migration (ADR 0024): the Redis
// listing honors only the user_name and ipaddr idents.
type ListSessionsRequest struct {
	Filter  filtering.Filter
	OrderBy ordering.OrderBy
	Limit   int
	Offset  int
}

// LogoutOwnedRequest revokes one Session after an atomic ownership check:
// the Session ID and the System User claiming it.
type LogoutOwnedRequest struct {
	SystemUserID int64
	SessionID    string
}

// RevokeSystemUsersRequest atomically revokes every Login Session of the
// given System Users except one optional Session ID (empty revokes all).
type RevokeSystemUsersRequest struct {
	SystemUserIDs   []int64
	ExceptSessionID string
}

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
	List(ctx context.Context, req ListSessionsRequest) ([]*entity.LoginSession, error)
	ListBySystemUser(ctx context.Context, systemUserID int64) ([]*entity.LoginSession, error)
	// Kickout force-revokes one Session ID for an administrator.
	Kickout(ctx context.Context, sessionID string) error
	// LogoutOwned revokes one Session after an atomic ownership check against
	// the System User.
	LogoutOwned(ctx context.Context, req LogoutOwnedRequest) error
	// RevokeSystemUsers atomically revokes every Login Session of the given
	// System Users except one optional Session ID (empty revokes all).
	RevokeSystemUsers(ctx context.Context, req RevokeSystemUsersRequest) error
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

func (s *onlineSessionImpl) List(ctx context.Context, req ListSessionsRequest) ([]*entity.LoginSession, error) {
	sessions, err := s.sessions.List(ctx,
		repository.ListFilter(req.Filter),
		repository.ListOrderBy(req.OrderBy),
		repository.ListLimit(req.Limit),
		repository.ListOffset(req.Offset),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.list_online_sessions", err)
		return nil, err
	}
	return sessions, nil
}

func (s *onlineSessionImpl) ListBySystemUser(ctx context.Context, systemUserID int64) ([]*entity.LoginSession, error) {
	sessions, err := s.sessions.ListBySystemUser(ctx, repository.ListBySystemUserRequest{SystemUserID: systemUserID})
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

func (s *onlineSessionImpl) LogoutOwned(ctx context.Context, req LogoutOwnedRequest) error {
	if err := s.sessions.LogoutOwned(ctx, repository.SessionOwnershipRequest{
		SystemUserID: req.SystemUserID,
		SessionID:    req.SessionID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.logout_owned", err)
		return err
	}
	return nil
}

func (s *onlineSessionImpl) RevokeSystemUsers(ctx context.Context, req RevokeSystemUsersRequest) error {
	if err := s.sessions.RevokeSystemUsers(ctx, repository.RevokeSystemUsersRequest{
		SystemUserIDs:   req.SystemUserIDs,
		ExceptSessionID: req.ExceptSessionID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "authn.svc.revoke_system_users", err)
		return err
	}
	return nil
}
