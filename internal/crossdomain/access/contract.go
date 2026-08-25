// Package access publishes access's cross-module contracts in the
// anti-corruption shape of ADR 0017: contract.go holds the consumer-shaped
// interface, model/ holds flat snapshot types, impl/ adapts the provider's
// domain capability, and accessmock holds the mockgen-generated test double.
// Contracts are consumed by constructor injection into the calling Usecase;
// there is deliberately no package-level service locator, and cmd stays the
// only composition root.
package access

import (
	"context"

	"glex/internal/crossdomain/access/model"
)

//go:generate go tool mockgen -package accessmock -destination accessmock/authorization_resolver.go glex/internal/crossdomain/access AuthorizationResolver

// AuthorizationResolver resolves the authorization content an Authorization
// Snapshot captures for a System User: active Role Keys and Functional
// Permissions computed from the user's Role Assignments and Menu Grants. The
// consumer (authn's login Usecase) never derives permissions itself and never
// imports the provider's domain package (ADR 0016/0017).
type AuthorizationResolver interface {
	ResolveAuthorization(ctx context.Context, systemUserID int64) (*model.AuthorizationResolution, error)
}
