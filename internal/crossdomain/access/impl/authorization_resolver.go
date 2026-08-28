// Package impl adapts access's domain capabilities to the crossdomain
// contracts. Adapters live here rather than in infra (ADR 0005): this is the
// only place that knows both the contract snapshots and the provider's domain
// services.
package impl

import (
	"context"

	"glex/internal/crossdomain/access"
	"glex/internal/crossdomain/access/model"
	accessservice "glex/internal/domain/access/service"
)

// authorizationResolver satisfies the resolution contract by delegating to
// the provider's Authorization Resolution domain service.
type authorizationResolver struct {
	resolver accessservice.Authorization
}

// NewAuthorizationResolver adapts the access domain's Authorization
// Resolution service to the crossdomain resolution contract.
func NewAuthorizationResolver(resolver accessservice.Authorization) access.AuthorizationResolver {
	return &authorizationResolver{resolver: resolver}
}

// ResolveAuthorization translates the provider's resolution onto the flat
// model snapshot. Provider failures pass through unchanged; the consumer maps
// them onto its own availability vocabulary.
func (a *authorizationResolver) ResolveAuthorization(ctx context.Context, systemUserID int64) (*model.AuthorizationResolution, error) {
	resolution, err := a.resolver.ResolveAuthorization(ctx, systemUserID)
	if err != nil {
		return nil, err
	}
	if resolution == nil {
		return &model.AuthorizationResolution{}, nil
	}
	return &model.AuthorizationResolution{
		RoleKeys:    resolution.RoleKeys,
		Permissions: resolution.Permissions,
	}, nil
}
