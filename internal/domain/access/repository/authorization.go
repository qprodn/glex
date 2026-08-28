package repository

import (
	"context"

	"glex/internal/domain/access/entity"
)

// AuthorizationResolver resolves the authorization content captured by an
// Authorization Snapshot. Its only consumer is authn's login Application
// service, reached through the crossdomain access contract (ADR 0005) — never
// by importing this package.
type AuthorizationResolver interface {
	// ResolveAuthorization freshly reads the System User's active Role
	// Assignments and Menu Grants and computes the resolution. Reads join the
	// transaction propagated through ctx when one is active.
	ResolveAuthorization(ctx context.Context, systemUserID int64) (*entity.AuthorizationResolution, error)
}
