package repository

import (
	"context"

	"glex/internal/domain/authn/entity"
)

// Authenticator is the transport-neutral authentication capability implemented
// by data and consumed by HTTP middleware.
type Authenticator interface {
	Authenticate(ctx context.Context, bearerToken string) (*entity.Principal, error)
}
