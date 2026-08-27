// Package repository holds the access module's ports: the repository and
// capability interfaces, their request/response shapes, and the ListOption
// family.
package repository

import (
	"context"

	"glex/internal/domain/access/entity"
)

// MenuRouteAccess is the freshly loaded authorization data used to build one
// System User's route tree. It deliberately carries menu ids rather than
// Functional Permission handles: routes follow active Role Assignments and
// Menu Grants, while permission checks use the separate Authorization
// Snapshot.
type MenuRouteAccess struct {
	RoleKeys       []string
	GrantedMenuIDs []int64
	ActiveMenus    []*entity.Menu
}

// MenuUpdate pairs the persisted Menu with its pre-image and the active
// System Users reaching the menu through active Roles — identities whose
// Authorization Snapshot the write may invalidate.
type MenuUpdate struct {
	Menu            *entity.Menu
	Previous        *entity.Menu
	AffectedUserIDs []int64
}

// MenuRepo is the repository interface for menus (the inversion seam declared
// in domain, implemented in data).
type MenuRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Menu, error)
	FindByParentAndName(ctx context.Context, parentID int64, name string) (*entity.Menu, error)
	HasChildren(ctx context.Context, id int64) (bool, error)
	// ListByIDs returns the menus with the given ids that exist and are not
	// soft-deleted (used to validate Menu Grants).
	ListByIDs(ctx context.Context, ids []int64) ([]*entity.Menu, error)
	ListMenus(ctx context.Context, opts ...ListOption) ([]*entity.Menu, error)
	// LoadRouteAccess freshly reads the System User's active Role Assignments,
	// their Menu Grants, and every active Directory/Menu. Button nodes are not
	// included in ActiveMenus.
	LoadRouteAccess(ctx context.Context, systemUserID int64) (*MenuRouteAccess, error)
	CreateMenu(ctx context.Context, m *entity.Menu) (*entity.Menu, error)
	// UpdateMenu returns the persisted menu with its pre-image and the users
	// currently affected. The Application service owns the transaction.
	UpdateMenu(ctx context.Context, m *entity.Menu) (*MenuUpdate, error)
	// DeleteMenu is a security write: it soft-deletes the menu and its role
	// links (ADR 0002 — no row is hard-deleted), stamping actorID into the
	// audit fields, and returns the System User identities reaching the menu.
	DeleteMenu(ctx context.Context, id int64, actorID int64) ([]int64, error)
}
