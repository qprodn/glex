// Package entity holds the access module's domain objects and value objects.
// Pure domain models: no proto, no storage tags.
package entity

import (
	"time"

	"glex/internal/domain/access"
)

// MenuType values for Menu.MenuType (mirror of the api/access/v1 MenuType
// enum). The DO carries the int32 so domain stays free of proto types.
const (
	MenuTypeDirectory int32 = 1 // 目录
	MenuTypeMenu      int32 = 2 // 菜单
	MenuTypeButton    int32 = 3 // 按钮
)

// Menu is a menu-node domain object (菜单), ported from RuoYi sys_menu. Children
// is populated by service.BuildMenuTree when serving the tree; it is nil for a
// freshly loaded or created menu.
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
		return access.ErrMenuTypeInvalid
	}
	if m.ID != 0 && m.ParentID == m.ID {
		return access.ErrMenuParentInvalid
	}
	return nil
}

// AuthorizationChangedFrom classifies Menu writes that invalidate snapshots.
func (m *Menu) AuthorizationChangedFrom(current *Menu) bool {
	return m != nil && current != nil && (m.Perms != current.Perms || m.Status != current.Status)
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
