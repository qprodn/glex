package service

import (
	"context"

	"glex/internal/domain/authn/entity"
	authmw "glex/pkg/middleware/auth"
)

type principalContextKey struct{}

// WithPrincipal publishes an authenticated Principal through the auth
// middleware's typed context key. It is primarily used by that middleware.
func WithPrincipal(ctx context.Context, principal *entity.Principal) context.Context {
	if principal == nil {
		return ctx
	}
	return authmw.WithPrincipal(ctx, principal)
}

// PrincipalFromContext returns the authenticated identity, when present.
func PrincipalFromContext(ctx context.Context) (*entity.Principal, bool) {
	principal, ok := authmw.PrincipalFromContext(ctx)
	typed, isBiz := principal.(*entity.Principal)
	return typed, ok && isBiz
}
