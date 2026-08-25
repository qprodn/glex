// Package authn publishes authn's cross-module contracts in the anti-corruption
// shape of ADR 0017: contract.go holds the consumer-shaped interface, model/
// holds flat snapshot types, impl/ adapts the provider's domain capability, and
// authnmock holds the mockgen-generated test double. Contracts are consumed by
// constructor injection into the calling Usecase; there is deliberately no
// package-level service locator, and cmd stays the only composition root.
package authn

import (
	"context"

	"glex/internal/crossdomain/authn/model"
)

//go:generate go tool mockgen -package authnmock -destination authnmock/session_revoker.go glex/internal/crossdomain/authn SessionRevoker

// SessionRevoker revokes Login Sessions after a security-changing write in a
// consuming module. The request is a flat model snapshot; the provider's
// domain types never cross this seam.
type SessionRevoker interface {
	RevokeSystemUsers(ctx context.Context, request *model.RevocationRequest) error
}
