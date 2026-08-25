package access

import (
	"context"
	"sort"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
)

// MenuType values for Menu.MenuType (mirror of the api/iam/menu/v1 MenuType
// enum). The DO carries the int32 so domain stays free of proto types.
const (
	MenuTypeDirectory int32 = 1 // 目录
	MenuTypeMenu      int32 = 2 // 菜单
	MenuTypeButton    int32 = 3 // 按钮
)

// Menu is a menu-node domain object (菜单), ported from RuoYi sys_menu. Pure domain
// model: no proto, no storage tags. Children is populated by buildTree when
// serving the tree; it is nil for a freshly loaded or created menu.
type Menu struct {
	ID         int64
	MenuName   string
	ParentID   int64
	OrderNum   int
	Path       string
	Component  string
	QueryParam string
	IsFrame    int32
	IsCache    int32
	MenuType   int32
	Visible    int32
	Status     int32
	Perms      string
	Icon       string
	ActiveMenu string
	Ext        string
	CreateBy   int64
	UpdateBy   int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Children   []*Menu
}

// Validate enforces the invariants a Menu must satisfy before persistence.
func (m *Menu) Validate() error {
	if m == nil || m.MenuType < MenuTypeDirectory || m.MenuType > MenuTypeButton {
		return ErrMenuTypeInvalid
	}
	if m.ID != 0 && m.ParentID == m.ID {
		return ErrMenuParentInvalid
	}
	return nil
}

// AuthorizationChangedFrom classifies Menu writes that invalidate snapshots.
func (m *Menu) AuthorizationChangedFrom(current *Menu) bool {
	return m != nil && current != nil && (m.Perms != current.Perms || m.Status != current.Status)
}

// BuildMenuTree assembles an ordered forest from a flat menu list. Orphans
// remain absent from the returned roots.
func BuildMenuTree(menus []*Menu) []*Menu {
	byParent := make(map[int64][]*Menu)
	for _, menu := range menus {
		if menu != nil {
			byParent[menu.ParentID] = append(byParent[menu.ParentID], menu)
		}
	}
	for _, siblings := range byParent {
		sort.SliceStable(siblings, func(i, j int) bool { return siblings[i].OrderNum < siblings[j].OrderNum })
	}
	roots := byParent[0]
	var attach func([]*Menu)
	attach = func(nodes []*Menu) {
		for _, node := range nodes {
			node.Children = byParent[node.ID]
			attach(node.Children)
		}
	}
	attach(roots)
	return roots
}

// SelectUserRouteTree resolves the active, granted route menus and their
// ancestors, then returns them as an ordered tree.
func SelectUserRouteTree(access *MenuRouteAccess) []*Menu {
	if access == nil {
		return []*Menu{}
	}
	all := make(map[int64]*Menu, len(access.ActiveMenus))
	for _, menu := range access.ActiveMenus {
		if menu != nil {
			all[menu.ID] = menu
		}
	}
	selected := make(map[int64]*Menu, len(all))
	if containsRoleKey(access.RoleKeys, "superadmin") {
		for id, menu := range all {
			selected[id] = menu
		}
	} else {
		for _, id := range access.GrantedMenuIDs {
			menu, ok := all[id]
			if !ok {
				continue
			}
			selected[id] = menu
			seen := map[int64]struct{}{menu.ID: {}}
			for parentID := menu.ParentID; parentID != 0; {
				if _, cycle := seen[parentID]; cycle {
					break
				}
				seen[parentID] = struct{}{}
				parent, ok := all[parentID]
				if !ok {
					break
				}
				selected[parent.ID] = parent
				parentID = parent.ParentID
			}
		}
	}
	menus := make([]*Menu, 0, len(selected))
	for _, menu := range access.ActiveMenus {
		if menu != nil {
			if _, ok := selected[menu.ID]; ok {
				menus = append(menus, menu)
			}
		}
	}
	return BuildMenuTree(menus)
}

func containsRoleKey(roleKeys []string, want string) bool {
	for _, roleKey := range roleKeys {
		if roleKey == want {
			return true
		}
	}
	return false
}

// MenuRouteAccess is the freshly loaded authorization data used to build one
// System User's route tree. It deliberately carries menu ids rather than
// Functional Permission handles: routes follow active Role Assignments and
// Menu Grants, while permission checks use the separate Authorization
// Snapshot.
type MenuRouteAccess struct {
	RoleKeys       []string
	GrantedMenuIDs []int64
	ActiveMenus    []*Menu
}

// MenuUpdate pairs the persisted Menu with its pre-image and the active
// System Users reaching the menu through active Roles — identities whose
// Authorization Snapshot the write may invalidate.
type MenuUpdate struct {
	Menu            *Menu
	Previous        *Menu
	AffectedUserIDs []int64
}

// MenuRepo is the repository interface for menus (the inversion seam declared
// in domain, implemented in data).
type MenuRepo interface {
	FindByID(ctx context.Context, id int64) (*Menu, error)
	FindByParentAndName(ctx context.Context, parentID int64, name string) (*Menu, error)
	HasChildren(ctx context.Context, id int64) (bool, error)
	// ListByIDs returns the menus with the given ids that exist and are not
	// soft-deleted (used to validate Menu Grants).
	ListByIDs(ctx context.Context, ids []int64) ([]*Menu, error)
	ListMenus(ctx context.Context, opts ...ListOption) ([]*Menu, error)
	// LoadRouteAccess freshly reads the System User's active Role Assignments,
	// their Menu Grants, and every active Directory/Menu. Button nodes are not
	// included in ActiveMenus.
	LoadRouteAccess(ctx context.Context, systemUserID int64) (*MenuRouteAccess, error)
	CreateMenu(ctx context.Context, m *Menu) (*Menu, error)
	// UpdateMenu returns the persisted menu with its pre-image and the users
	// currently affected. The Application Usecase owns the transaction.
	UpdateMenu(ctx context.Context, m *Menu) (*MenuUpdate, error)
	// DeleteMenu is a security write: it soft-deletes the menu and
	// hard-deletes its role links, and returns
	// the System User identities reaching the menu.
	DeleteMenu(ctx context.Context, id int64) ([]int64, error)
}

var (
	// ErrMenuNotFound: the requested menu does not exist.
	ErrMenuNotFound = errors.NotFound("MENU_NOT_FOUND", "menu not found")
	// ErrMenuHasChildren: the menu still has children and cannot be deleted.
	ErrMenuHasChildren = errors.BadRequest("MENU_HAS_CHILDREN", "menu has children")
	// ErrMenuNameConflict: menu_name already exists under the same parent.
	ErrMenuNameConflict = errors.Conflict("MENU_NAME_CONFLICT", "menu_name already exists under this parent")
	// ErrMenuParentInvalid: a menu cannot be its own parent.
	ErrMenuParentInvalid = errors.BadRequest("MENU_PARENT_INVALID", "a menu cannot be its own parent")
	// ErrMenuTypeInvalid: menu_type is not a valid MenuType.
	ErrMenuTypeInvalid = errors.BadRequest("MENU_TYPE_INVALID", "invalid menu_type")
)
