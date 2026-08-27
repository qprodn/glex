// Package impl adapts access's domain capabilities to the crossdomain
// contracts. Adapters live here rather than in infra (ADR 0017): this is the
// only place that knows both the contract snapshots and the provider's domain
// types.
package impl

import (
	"context"

	"glex/internal/crossdomain/access"
	"glex/internal/crossdomain/access/model"
	accessrepo "glex/internal/domain/access/repository"
)

// authorizationResolver satisfies the resolution contract by wrapping the
// provider's Authorization Resolution capability.
type authorizationResolver struct {
	resolver accessrepo.AuthorizationResolver
}

// NewAuthorizationResolver adapts the access domain's AuthorizationResolver
// port to the crossdomain resolution contract.
func NewAuthorizationResolver(resolver accessrepo.AuthorizationResolver) access.AuthorizationResolver {
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
