package entity

import (
	"time"

	"glex/internal/domain/access"
)

// Role is a role domain object (角色), ported from RuoYi sys_role.
// Carries a Menu Grant (菜单授权) via the repo; user↔role assignment is deferred
// to the role-assignment operations.
type Role struct {
	ID                int64
	RoleName          string
	RoleKey           string
	RoleSort          int
	Status            int32
	MenuCheckStrictly bool
	Remark            string
	CreateBy          int64
	UpdateBy          int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Validate enforces the invariants a Role must satisfy before persistence.
func (r *Role) Validate() error {
	if r == nil || r.RoleName == "" || r.RoleKey == "" {
		return access.ErrRoleInvalid
	}
	return nil
}

// AuthorizationChangedFrom classifies Role writes that invalidate snapshots.
func (r *Role) AuthorizationChangedFrom(current *Role) bool {
	return r != nil && current != nil && (r.RoleKey != current.RoleKey || r.Status != current.Status)
}

// RoleUpdate pairs the persisted Role with its pre-image and the active
// System Users assigned it — identities whose Authorization Snapshot the
// write may invalidate.
type RoleUpdate struct {
	Role            *Role
	Previous        *Role
	AssignedUserIDs []int64
}
