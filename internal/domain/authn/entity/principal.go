// Package entity holds the authn module's domain objects and value objects.
// Pure domain models: no proto, no storage tags.
package entity

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
