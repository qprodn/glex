// Package repository holds the access module's ports: the repository and
// capability interfaces, their request/response shapes, and the ListOption
// family.
package repository

import (
	"context"

	"glex/internal/domain/access/entity"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// FindByParentAndNameRequest names the (parent_id, menu_name) pair resolved
// among non-withdrawn menus.
type FindByParentAndNameRequest struct {
	ParentID int64
	Name     string
}

// ListMenusRequest is one menu-list read: the raw AIP-160 filter and AIP-132
// order (no pagination — menus list unpaginated as a small, tree-shaped set).
type ListMenusRequest struct {
	Filter  filtering.Filter
	OrderBy ordering.OrderBy
}

// DeleteMenuRequest identifies the menu a Delete withdraws and the actor
// whose id lands in the audit fields.
type DeleteMenuRequest struct {
	ID      int64
	ActorID int64
}

// MenuRepo is the repository interface for menus (the inversion seam declared
// in domain, implemented in data).
type MenuRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Menu, error)
	// FindByParentAndName returns the menu named req.Name under req.ParentID —
	// the sibling-uniqueness lookup. ErrMenuNotFound means "available".
	FindByParentAndName(ctx context.Context, req FindByParentAndNameRequest) (*entity.Menu, error)
	HasChildren(ctx context.Context, id int64) (bool, error)
	// ListByIDs returns the menus with the given ids that exist and are not
	// soft-deleted (used to validate Menu Grants).
	ListByIDs(ctx context.Context, ids []int64) ([]*entity.Menu, error)
	ListMenus(ctx context.Context, req ListMenusRequest) ([]*entity.Menu, error)
	// LoadRouteAccess freshly reads the System User's active Role Assignments,
	// their Menu Grants, and every active Directory/Menu. Button nodes are not
	// included in ActiveMenus.
	LoadRouteAccess(ctx context.Context, systemUserID int64) (*entity.MenuRouteAccess, error)
	CreateMenu(ctx context.Context, m *entity.Menu) (*entity.Menu, error)
	// UpdateMenu returns the persisted menu with its pre-image and the users
	// currently affected. The Application service owns the transaction.
	UpdateMenu(ctx context.Context, m *entity.Menu) (*entity.MenuUpdate, error)
	// DeleteMenu is a security write: it soft-deletes the menu identified by
	// req and its role links (ADR 0002 — no row is hard-deleted), stamping
	// req.ActorID into the audit fields, and returns the System User
	// identities reaching the menu.
	DeleteMenu(ctx context.Context, req DeleteMenuRequest) ([]int64, error)
}
