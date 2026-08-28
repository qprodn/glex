package service

import (
	"context"

	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"
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
}

// NewAuthorizationService creates the Authorization Resolution domain service
// over the module's resolution capability.
func NewAuthorizationService(resolver repository.AuthorizationResolver) Authorization {
	return &authorizationImpl{resolver: resolver}
}

func (s *authorizationImpl) ResolveAuthorization(ctx context.Context, systemUserID int64) (*entity.AuthorizationResolution, error) {
	return s.resolver.ResolveAuthorization(ctx, systemUserID)
}
