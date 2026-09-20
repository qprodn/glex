// Package auth provides a kratos middleware that authenticates every request
// carrying a bearer token and enforces per-operation checks declared in a
// route table keyed by operation name. An operation absent from the table is
// public: no check runs, though a valid token still publishes the principal.
package auth

import (
	"context"
	"strings"

	"github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

// Principal is the authenticated identity the middleware checks against.
type Principal interface {
	// HasPermission reports whether the principal holds one Functional
	// Permission handle.
	HasPermission(permission string) bool
	// HasRole reports whether the principal holds one role key.
	HasRole(roleKey string) bool
}

// Authenticator resolves a bearer credential into a Principal.
type Authenticator interface {
	Authenticate(ctx context.Context, bearerToken string) (Principal, error)
}

// Route declares the checks one operation requires. The zero value enforces
// nothing; Permission and Role each imply login. No wildcard matching — every
// handle and role key is an exact match.
type Route struct {
	// Login requires an authenticated principal.
	Login bool
	// Permission requires this Functional Permission handle.
	Permission string
	// Role requires this role key.
	Role string
}

func (r Route) enforced() bool {
	return r.Login || r.Permission != "" || r.Role != ""
}

var (
	ErrCredentialMissing      = errors.Unauthorized("CREDENTIAL_MISSING", "Authorization credential is required")
	ErrCredentialMalformed    = errors.Unauthorized("CREDENTIAL_MALFORMED", "Authorization credential is malformed")
	ErrTokenInvalid           = errors.Unauthorized("TOKEN_INVALID", "invalid token")
	ErrTokenExpired           = errors.Unauthorized("TOKEN_EXPIRED", "token expired")
	ErrAuthBackendUnavailable = errors.ServiceUnavailable("AUTH_BACKEND_UNAVAILABLE", "authentication backend unavailable")
	ErrPermissionDenied       = errors.Forbidden("PERMISSION_DENIED", "Functional Permission denied")
	ErrRoleDenied             = errors.Forbidden("ROLE_DENIED", "role denied")
)

// Middleware authenticates each HTTP request and enforces the route table.
func Middleware(authenticator Authenticator, routes map[string]Route) middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			transporter, ok := transport.FromServerContext(ctx)
			if !ok || transporter.Kind() != transport.KindHTTP {
				return next(ctx, req)
			}
			route := routes[transporter.Operation()]
			header := transporter.RequestHeader()
			authorization := ""
			if header != nil {
				authorization = header.Get("Authorization")
			}
			if authorization == "" {
				if route.enforced() {
					return nil, ErrCredentialMissing
				}
				return next(ctx, req)
			}
			token, err := parseBearerCredential(authorization)
			if err != nil {
				if route.enforced() {
					return nil, err
				}
				return next(ctx, req)
			}
			if authenticator == nil {
				return nil, ErrAuthBackendUnavailable
			}
			principal, err := authenticator.Authenticate(ctx, token)
			if err != nil {
				if route.enforced() {
					return nil, err
				}
				return next(ctx, req)
			}
			if principal == nil {
				return nil, ErrAuthBackendUnavailable
			}
			if route.Permission != "" && !principal.HasPermission(route.Permission) {
				return nil, ErrPermissionDenied
			}
			if route.Role != "" && !principal.HasRole(route.Role) {
				return nil, ErrRoleDenied
			}
			return next(WithPrincipal(ctx, principal), req)
		}
	}
}

func parseBearerCredential(header string) (string, error) {
	separator := strings.IndexByte(header, ' ')
	if separator <= 0 || !strings.EqualFold(header[:separator], "Bearer") {
		return "", ErrCredentialMalformed
	}
	remaining := header[separator:]
	for len(remaining) > 0 && remaining[0] == ' ' {
		remaining = remaining[1:]
	}
	if remaining == "" || strings.IndexAny(remaining, " \t\r\n") >= 0 {
		return "", ErrCredentialMalformed
	}
	return remaining, nil
}

type principalContextKey struct{}

// WithPrincipal publishes an authenticated Principal through a private typed
// context key. It is primarily used by the auth middleware.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	if principal == nil {
		return ctx
	}
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext returns the authenticated identity, when present —
// nil means no Principal is attached (ADR 0025: missing is nil, never a bool
// flag).
func PrincipalFromContext(ctx context.Context) Principal {
	principal, _ := ctx.Value(principalContextKey{}).(Principal)
	return principal
}
