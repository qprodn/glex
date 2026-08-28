package repository

import (
	"context"

	"glex/internal/domain/access/entity"
)

// RoleMenuLink is one row of a role's Menu Grant as read from storage: the
// granted menu id and its parent id. The read path uses ParentID to shape
// checkedKeys in the access service.
type RoleMenuLink struct {
	MenuID   int64
	ParentID int64
}

// RoleRepo is the repository interface for roles (the inversion seam declared
// in domain, implemented in data).
type RoleRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Role, error)
	FindByRoleName(ctx context.Context, roleName string) (*entity.Role, error)
	FindByRoleKey(ctx context.Context, roleKey string) (*entity.Role, error)
	ListRoles(ctx context.Context, opts ...ListOption) ([]*entity.Role, error)
	// CreateRole persists the role and its initial Menu Grant. menuIDs are
	// stored verbatim after the caller validates and dedupes them.
	CreateRole(ctx context.Context, r *entity.Role, menuIDs []int64) (*entity.Role, error)
	// UpdateRole returns the persisted role with its pre-image and currently
	// assigned users. The Application service owns the transaction.
	UpdateRole(ctx context.Context, r *entity.Role) (*entity.RoleUpdate, error)
	// DeleteRole is a security write: it soft-deletes the role and its Menu
	// Grant (ADR 0002 — no row is hard-deleted), stamping actorID into the
	// audit fields, and returns the System User identities assigned the role
	// at delete time.
	DeleteRole(ctx context.Context, id int64, actorID int64) ([]int64, error)
	// ListRoleMenuLinks returns the role's granted menus with their parent
	// ids, ordered by (parent_id, order_num); soft-deleted menus excluded.
	ListRoleMenuLinks(ctx context.Context, roleID int64) ([]*RoleMenuLink, error)
	// ReplaceRoleMenus is a security write: it atomically replaces the role's
	// active Menu Grant with menuIDs and writes menuCheckStrictly (full
	// replacement of the active set), stamping actorID into the audit fields,
	// and returns the System User identities currently assigned the role.
	ReplaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool, actorID int64) ([]int64, error)
}
