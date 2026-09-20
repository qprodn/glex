package repository

import (
	"context"

	"glex/internal/domain/access/entity"
	"glex/pkg/listq"
)

// RoleMenuLink is one row of a role's Menu Grant as read from storage: the
// granted menu id and its parent id. The read path uses ParentID to shape
// checkedKeys in the access service.
type RoleMenuLink struct {
	MenuID   int64
	ParentID int64
}

// CreateRoleRequest is one role creation: the role to persist and the menu
// ids of its initial Menu Grant.
type CreateRoleRequest struct {
	Role    *entity.Role
	MenuIDs []int64
}

// DeleteRoleRequest identifies the role a Delete withdraws and the actor
// whose id lands in the audit fields.
type DeleteRoleRequest struct {
	ID      int64
	ActorID int64
}

// ReplaceRoleMenusRequest is one full replacement of a role's Menu Grant:
// the role, the menu ids to store, the menu_check_strictly flag written
// alongside them, and the actor stamped into the audit fields.
type ReplaceRoleMenusRequest struct {
	RoleID            int64
	MenuIDs           []int64
	MenuCheckStrictly bool
	ActorID           int64
}

// RoleRepo is the repository interface for roles (the inversion seam declared
// in domain, implemented in data).
type RoleRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Role, error)
	FindByRoleName(ctx context.Context, roleName string) (*entity.Role, error)
	FindByRoleKey(ctx context.Context, roleKey string) (*entity.Role, error)
	ListRoles(ctx context.Context, q listq.Query) ([]*entity.Role, error)
	// CreateRole persists req.Role and its initial Menu Grant (req.MenuIDs are
	// stored verbatim after the caller validates and dedupes them).
	CreateRole(ctx context.Context, req CreateRoleRequest) (*entity.Role, error)
	// UpdateRole returns the persisted role with its pre-image and currently
	// assigned users. The Application service owns the transaction.
	UpdateRole(ctx context.Context, r *entity.Role) (*entity.RoleUpdate, error)
	// DeleteRole is a security write: it soft-deletes the role identified by
	// req and its Menu Grant (ADR 0002 — no row is hard-deleted), stamping
	// req.ActorID into the audit fields, and returns the System User
	// identities assigned the role at delete time.
	DeleteRole(ctx context.Context, req DeleteRoleRequest) ([]int64, error)
	// ListRoleMenuLinks returns the role's granted menus with their parent
	// ids, ordered by (parent_id, order_num); soft-deleted menus excluded.
	ListRoleMenuLinks(ctx context.Context, roleID int64) ([]*RoleMenuLink, error)
	// ReplaceRoleMenus is a security write: it atomically replaces the role's
	// active Menu Grant with req.MenuIDs and writes req.MenuCheckStrictly
	// (full replacement of the active set), stamping req.ActorID into the
	// audit fields, and returns the System User identities currently assigned
	// the role.
	ReplaceRoleMenus(ctx context.Context, req ReplaceRoleMenusRequest) ([]int64, error)
}
