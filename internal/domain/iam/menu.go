package iam

import (
	"context"
	"time"
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
// Authorization Snapshot the write may invalidate, discovered under the
// authorization locks.
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
	// UpdateMenu is a security write: it takes the Menu, Role, and System User
	// authorization locks, mutates under them, and returns the persisted menu
	// with its pre-image and the affected users. Pair the result with
	// revocation inside Transaction.InTx (see MenuUsecase.UpdateMenu).
	UpdateMenu(ctx context.Context, m *Menu) (*MenuUpdate, error)
	// DeleteMenu is a security write: it soft-deletes the menu and
	// hard-deletes its role links under the authorization locks, and returns
	// the System User identities reaching the menu.
	DeleteMenu(ctx context.Context, id int64) ([]int64, error)
}
