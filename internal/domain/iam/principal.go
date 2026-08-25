package iam

import (
	"context"

	"github.com/go-kratos/kratos/v3/errors"

	authmw "glex/pkg/middleware/auth"
)

const superadminPermission = "*:*:*"

// Principal is the immutable authenticated identity made available to
// transport handlers. The bearer credential is deliberately absent.
type Principal struct {
	systemUserID int64
	userName     string
	sessionID    string
	roleKeys     []string
	permissions  []string
}

// NewPrincipal copies every collection so callers cannot mutate the identity
// after it has been authenticated.
func NewPrincipal(systemUserID int64, userName, sessionID string, roleKeys, permissions []string) *Principal {
	return &Principal{
		systemUserID: systemUserID,
		userName:     userName,
		sessionID:    sessionID,
		roleKeys:     append([]string(nil), roleKeys...),
		permissions:  append([]string(nil), permissions...),
	}
}

func (p *Principal) SystemUserID() int64 { return p.systemUserID }
func (p *Principal) UserName() string    { return p.userName }
func (p *Principal) SessionID() string   { return p.sessionID }

func (p *Principal) RoleKeys() []string {
	return append([]string(nil), p.roleKeys...)
}

func (p *Principal) Permissions() []string {
	return append([]string(nil), p.permissions...)
}

// HasPermission checks the single Functional Permission required by one RPC.
// Only the explicitly stored superadmin wildcard bypasses an exact match.
func (p *Principal) HasPermission(permission string) bool {
	for _, granted := range p.permissions {
		if granted == permission || granted == superadminPermission {
			return true
		}
	}
	return false
}

// HasRole checks the single role key required by one RPC with a strict
// match; the superadmin permission wildcard never satisfies a role check.
func (p *Principal) HasRole(roleKey string) bool {
	for _, granted := range p.roleKeys {
		if granted == roleKey {
			return true
		}
	}
	return false
}

type principalContextKey struct{}

// WithPrincipal publishes an authenticated Principal through the auth
// middleware's typed context key. It is primarily used by that middleware.
func WithPrincipal(ctx context.Context, principal *Principal) context.Context {
	if principal == nil {
		return ctx
	}
	return authmw.WithPrincipal(ctx, principal)
}

// PrincipalFromContext returns the authenticated identity, when present.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	principal, ok := authmw.PrincipalFromContext(ctx)
	typed, isBiz := principal.(*Principal)
	return typed, ok && isBiz
}

// Authenticator is the transport-neutral authentication capability implemented
// by data and consumed by HTTP middleware.
type Authenticator interface {
	Authenticate(ctx context.Context, bearerToken string) (*Principal, error)
}

var (
	// ErrCredentialMissing: no authenticated Principal is present in the
	// context.
	ErrCredentialMissing = errors.Unauthorized("CREDENTIAL_MISSING", "Authorization credential is required")
	// ErrCredentialMalformed: the Authorization header does not carry a single
	// bearer credential. Raised by the HTTP middleware's own mirror of this
	// error.
	ErrCredentialMalformed = errors.Unauthorized("CREDENTIAL_MALFORMED", "Authorization credential is malformed")
	// ErrPermissionDenied: the Principal lacks the Functional Permission an
	// RPC requires. Raised by the HTTP middleware's own mirror of this error.
	ErrPermissionDenied = errors.Forbidden("PERMISSION_DENIED", "Functional Permission denied")
)
