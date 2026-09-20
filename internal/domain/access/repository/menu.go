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

// MenuRepo is the repository interface for menus (the inversion seam declared
// in domain, implemented in data).
type MenuRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Menu, error)
	FindByParentAndName(ctx context.Context, parentID int64, name string) (*entity.Menu, error)
	HasChildren(ctx context.Context, id int64) (bool, error)
	// ListByIDs returns the menus with the given ids that exist and are not
	// soft-deleted (used to validate Menu Grants).
	ListByIDs(ctx context.Context, ids []int64) ([]*entity.Menu, error)
	ListMenus(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy) ([]*entity.Menu, error)
	// LoadRouteAccess freshly reads the System User's active Role Assignments,
	// their Menu Grants, and every active Directory/Menu. Button nodes are not
	// included in ActiveMenus.
	LoadRouteAccess(ctx context.Context, systemUserID int64) (*entity.MenuRouteAccess, error)
	CreateMenu(ctx context.Context, m *entity.Menu) (*entity.Menu, error)
	// UpdateMenu returns the persisted menu with its pre-image and the users
	// currently affected. The Application service owns the transaction.
	UpdateMenu(ctx context.Context, m *entity.Menu) (*entity.MenuUpdate, error)
	// DeleteMenu is a security write: it soft-deletes the menu and its role
	// links (ADR 0002 — no row is hard-deleted), stamping actorID into the
	// audit fields, and returns the System User identities reaching the menu.
	DeleteMenu(ctx context.Context, id int64, actorID int64) ([]int64, error)
}
