package access

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
)

// Role is a role domain object (角色), ported from RuoYi sys_role.
// Pure domain model: no proto, no storage tags. Carries a Menu Grant (菜单授权)
// via the repo; user↔role assignment is deferred to the role-assignment usecase.
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
		return ErrRoleInvalid
	}
	return nil
}

// AuthorizationChangedFrom classifies Role writes that invalidate snapshots.
func (r *Role) AuthorizationChangedFrom(current *Role) bool {
	return r != nil && current != nil && (r.RoleKey != current.RoleKey || r.Status != current.Status)
}

// CheckedMenuIDs shapes the persisted Menu Grant for the role editor. Under
// strict checking, linked parents of other linked menus are represented by
// their children and omitted from the returned checked keys.
func (r *Role) CheckedMenuIDs(links []*RoleMenuLink) []int64 {
	ids := make([]int64, 0, len(links))
	if r != nil && r.MenuCheckStrictly {
		parents := make(map[int64]struct{}, len(links))
		for _, link := range links {
			if link != nil && link.ParentID != 0 {
				parents[link.ParentID] = struct{}{}
			}
		}
		for _, link := range links {
			if link == nil {
				continue
			}
			if _, halfChecked := parents[link.MenuID]; !halfChecked {
				ids = append(ids, link.MenuID)
			}
		}
		return ids
	}
	for _, link := range links {
		if link != nil {
			ids = append(ids, link.MenuID)
		}
	}
	return ids
}

// RoleMenuLink is one row of a role's Menu Grant as read from storage: the
// granted menu id and its parent id. The read path uses ParentID to shape
// checkedKeys in the Application Usecase.
type RoleMenuLink struct {
	MenuID   int64
	ParentID int64
}

// RoleUpdate pairs the persisted Role with its pre-image and the active
// System Users assigned it — identities whose Authorization Snapshot the
// write may invalidate.
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
	// CreateRole persists the role and its initial Menu Grant. menuIDs are
	// stored verbatim after the Application Usecase validates and dedupes them.
	CreateRole(ctx context.Context, r *Role, menuIDs []int64) (*Role, error)
	// UpdateRole returns the persisted role with its pre-image and currently
	// assigned users. The Application Usecase owns the transaction.
	UpdateRole(ctx context.Context, r *Role) (*RoleUpdate, error)
	// DeleteRole is a security write: it soft-deletes the role and
	// hard-deletes its Menu Grant, and returns
	// the System User identities assigned the role at delete time.
	DeleteRole(ctx context.Context, id int64) ([]int64, error)
	// ListRoleMenuLinks returns the role's granted menus with their parent
	// ids, ordered by (parent_id, order_num); soft-deleted menus excluded.
	ListRoleMenuLinks(ctx context.Context, roleID int64) ([]*RoleMenuLink, error)
	// ReplaceRoleMenus is a security write: it atomically replaces the role's
	// Menu Grant with menuIDs and writes menuCheckStrictly (full replacement),
	// and returns the System User identities currently assigned the role.
	ReplaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) ([]int64, error)
}

var (
	// ErrRoleNotFound: the requested role does not exist.
	ErrRoleNotFound = errors.NotFound("ROLE_NOT_FOUND", "role not found")
	// ErrRoleNameConflict: role_name already exists.
	ErrRoleNameConflict = errors.Conflict("ROLE_NAME_CONFLICT", "role_name already exists")
	// ErrRoleKeyConflict: role_key already exists.
	ErrRoleKeyConflict = errors.Conflict("ROLE_KEY_CONFLICT", "role_key already exists")
	// ErrRoleInvalid: role_name/role_key is blank.
	ErrRoleInvalid = errors.BadRequest("ROLE_INVALID", "role_name and role_key are required")
	// ErrRoleMenuNotFound: one or more menu ids in a Menu Grant do not exist
	// (or are soft-deleted).
	ErrRoleMenuNotFound = errors.BadRequest("ROLE_MENU_NOT_FOUND", "one or more menu ids do not exist")
	// ErrRoleAssigned: the role still has users assigned and cannot be
	// deleted or disabled (RuoYi: 已分配，不能删除/禁用).
	ErrRoleAssigned = errors.BadRequest("ROLE_ASSIGNED", "role is assigned to users and cannot be deleted or disabled")
)
