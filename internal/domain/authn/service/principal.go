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

// PrincipalFromContext returns the authenticated identity, when present —
// nil means no biz Principal is attached (ADR 0025: missing is nil, never a
// bool flag).
func PrincipalFromContext(ctx context.Context) *entity.Principal {
	typed, _ := authmw.PrincipalFromContext(ctx).(*entity.Principal)
	return typed
}
