package service

import (
	"context"
	"log/slog"

	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"
	"glex/pkg/logx"
)

// Authorization is the access domain service that computes Authorization
// Resolution (授权解析): the active Role Keys and Functional Permissions
// derived from a System User's active Role Assignments and Menu Grants. The
// crossdomain access contract delegates here (ADR 0005).
type Authorization interface {
	ResolveAuthorization(ctx context.Context, systemUserID int64) (*entity.AuthorizationResolution, error)
}

type authorizationImpl struct {
	resolver repository.AuthorizationResolver
	logger   *slog.Logger
}

// NewAuthorizationService creates the Authorization Resolution domain service
// over the module's resolution capability.
func NewAuthorizationService(resolver repository.AuthorizationResolver, logger *slog.Logger) Authorization {
	return &authorizationImpl{
		resolver: resolver,
		logger:   logger.With(slog.String("component", "domain.access.svc")),
	}
}

func (s *authorizationImpl) ResolveAuthorization(ctx context.Context, systemUserID int64) (*entity.AuthorizationResolution, error) {
	resolution, err := s.resolver.ResolveAuthorization(ctx, systemUserID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "access.svc.resolve_authorization", err)
		return nil, err
	}
	return resolution, nil
}
