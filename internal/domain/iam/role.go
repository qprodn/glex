package iam

import (
	"context"
	"time"
)

// Role is a role domain object (角色), ported from RuoYi sys_role.
// Pure domain model: no proto, no storage tags. Carries a Menu Grant (菜单授权)
// via the repo; user↔role assignment is deferred to the auth module.
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

// RoleMenuLink is one row of a role's Menu Grant as read from storage: the
// granted menu id and its parent id. The read path uses ParentID to shape
// checkedKeys (see RoleUsecase.ListRoleMenus).
type RoleMenuLink struct {
	MenuID   int64
	ParentID int64
}

// RoleUpdate pairs the persisted Role with its pre-image and the active
// System Users assigned it — identities whose Authorization Snapshot the
// write may invalidate, discovered under the authorization locks.
type RoleUpdate struct {
	Role            *Role
	Previous        *Role
	AssignedUserIDs []int64
}

// RoleRepo is the repository interface for roles (the inversion seam declared
// in domain, implemented in data).
type RoleRepo interface {
	FindByID(ctx context.Context, id int64) (*Role, error)
	FindByRoleName(ctx context.Context, roleName string) (*Role, error)
	FindByRoleKey(ctx context.Context, roleKey string) (*Role, error)
	ListRoles(ctx context.Context, opts ...ListOption) ([]*Role, error)
	// CreateRole persists the role and its initial Menu Grant in one
	// transaction. menuIDs are stored verbatim (deduped and validated by the
	// usecase first).
	CreateRole(ctx context.Context, r *Role, menuIDs []int64) (*Role, error)
	// UpdateRole is a security write: it takes the Role and System User
	// authorization locks, mutates under them, and returns the persisted role
	// with its pre-image and assigned users. Pair the result with revocation
	// inside Transaction.InTx (see RoleUsecase.UpdateRole).
	UpdateRole(ctx context.Context, r *Role) (*RoleUpdate, error)
	// DeleteRole is a security write: it soft-deletes the role and
	// hard-deletes its Menu Grant under the authorization locks, and returns
	// the System User identities assigned the role at delete time.
	DeleteRole(ctx context.Context, id int64) ([]int64, error)
	// ListRoleMenuLinks returns the role's granted menus with their parent
	// ids, ordered by (parent_id, order_num); soft-deleted menus excluded.
	ListRoleMenuLinks(ctx context.Context, roleID int64) ([]*RoleMenuLink, error)
	// ReplaceRoleMenus is a security write: it atomically replaces the role's
	// Menu Grant with menuIDs and writes menuCheckStrictly (full replacement —
	// RuoYi's delete-then-insert) under the authorization locks, and returns
	// the System User identities assigned the role.
	ReplaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) ([]int64, error)
}
