package biz

import "context"

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

type principalContextKey struct{}

// WithPrincipal publishes an authenticated Principal through a private typed
// context key. It is primarily used by the project auth middleware.
func WithPrincipal(ctx context.Context, principal *Principal) context.Context {
	if principal == nil {
		return ctx
	}
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext returns the authenticated identity, when present.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(*Principal)
	return principal, ok && principal != nil
}

// Authenticator is the transport-neutral authentication capability implemented
// by data and consumed by HTTP middleware.
type Authenticator interface {
	Authenticate(ctx context.Context, bearerToken string) (*Principal, error)
}
