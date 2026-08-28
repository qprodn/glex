// Package service holds the access module's domain services: the only holders
// of the module's repository ports (ADR 0005). Application use cases call
// these interfaces and never touch a repository directly; the Application
// service remains the transaction root, so every method joins the caller's
// ambient transaction. Pure rule and shaping functions (tree assembly, route
// selection, checked-keys shaping) remain free functions and stay callable
// from anywhere.
package service

import (
	"context"
	"errors"
	"sort"

	"glex/internal/domain/access"
	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// Menu is the access domain service behind the menu endpoints: the only path
// to the Menu reads and writes (ADR 0005). Methods join the caller's ambient
// transaction; the Application service owns the transaction boundary and the
// cross-domain revocation.
type Menu interface {
	Get(ctx context.Context, id int64) (*entity.Menu, error)
	List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy) ([]*entity.Menu, error)
	// LoadRouteAccess freshly reads the System User's route authorization
	// data (active Role Assignments, Menu Grants, and routeable menus).
	LoadRouteAccess(ctx context.Context, systemUserID int64) (*entity.MenuRouteAccess, error)
	// Create validates the node (type, sibling-name uniqueness) and persists
	// it.
	Create(ctx context.Context, m *entity.Menu) (*entity.Menu, error)
	// Update validates the change (self-parent, type, sibling-name
	// uniqueness) and persists it, returning the write with its pre-image and
	// the users reaching the menu.
	Update(ctx context.Context, m *entity.Menu) (*entity.MenuUpdate, error)
	// Delete soft-deletes the menu and its role links, refusing while it has
	// children, and returns the identities reaching the menu.
	Delete(ctx context.Context, id, actorID int64) ([]int64, error)
}

type menuImpl struct {
	menus repository.MenuRepo
}

// NewMenuService creates the menu domain service.
func NewMenuService(menus repository.MenuRepo) Menu {
	return &menuImpl{menus: menus}
}

func (s *menuImpl) Get(ctx context.Context, id int64) (*entity.Menu, error) {
	return s.menus.FindByID(ctx, id)
}

func (s *menuImpl) List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy) ([]*entity.Menu, error) {
	return s.menus.ListMenus(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
	)
}

func (s *menuImpl) LoadRouteAccess(ctx context.Context, systemUserID int64) (*entity.MenuRouteAccess, error) {
	return s.menus.LoadRouteAccess(ctx, systemUserID)
}

func (s *menuImpl) Create(ctx context.Context, m *entity.Menu) (*entity.Menu, error) {
	if err := ValidateMenuType(m.MenuType); err != nil {
		return nil, err
	}
	if err := AssertSiblingNameUnique(ctx, s.menus, m, false); err != nil {
		return nil, err
	}
	return s.menus.CreateMenu(ctx, m)
}

func (s *menuImpl) Update(ctx context.Context, m *entity.Menu) (*entity.MenuUpdate, error) {
	if m.ParentID == m.ID {
		return nil, access.ErrMenuParentInvalid
	}
	if err := ValidateMenuType(m.MenuType); err != nil {
		return nil, err
	}
	if err := AssertSiblingNameUnique(ctx, s.menus, m, true); err != nil {
		return nil, err
	}
	return s.menus.UpdateMenu(ctx, m)
}

func (s *menuImpl) Delete(ctx context.Context, id, actorID int64) ([]int64, error) {
	has, err := s.menus.HasChildren(ctx, id)
	if err != nil {
		return nil, err
	}
	if has {
		return nil, access.ErrMenuHasChildren
	}
	return s.menus.DeleteMenu(ctx, id, actorID)
}

// ValidateMenuType rejects anything outside the MenuType enum range.
func ValidateMenuType(t int32) error {
	if t < entity.MenuTypeDirectory || t > entity.MenuTypeButton {
		return access.ErrMenuTypeInvalid
	}
	return nil
}

// AssertSiblingNameUnique checks no other menu shares (parent_id, menu_name).
// When excludeSelf is set (update), a match bearing the same id is allowed. A
// not-found from the repo is the expected "available" case.
func AssertSiblingNameUnique(ctx context.Context, repo repository.MenuRepo, m *entity.Menu, excludeSelf bool) error {
	existing, err := repo.FindByParentAndName(ctx, m.ParentID, m.MenuName)
	if err != nil {
		if errors.Is(err, access.ErrMenuNotFound) {
			return nil
		}
		return err
	}
	if excludeSelf && existing.ID == m.ID {
		return nil
	}
	return access.ErrMenuNameConflict
}

// BuildMenuTree assembles a flat list into a forest in a single pass: roots are
// nodes whose parent_id == 0; each node pulls its children from a map keyed by
// parent id; sibling groups are sorted by order_num. Orphans — whose parent is
// missing (e.g. soft-deleted) — are dropped from the tree (they remain in the
// store and are still reachable by id).
func BuildMenuTree(menus []*entity.Menu) []*entity.Menu {
	byParent := make(map[int64][]*entity.Menu)
	for _, m := range menus {
		byParent[m.ParentID] = append(byParent[m.ParentID], m)
	}
	for _, group := range byParent {
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].OrderNum < group[j].OrderNum
		})
	}
	roots := byParent[0]
	var attach func(nodes []*entity.Menu)
	attach = func(nodes []*entity.Menu) {
		for _, n := range nodes {
			n.Children = byParent[n.ID]
			attach(n.Children)
		}
	}
	attach(roots)
	return roots
}

// SelectUserRouteTree resolves the active, granted routeable menus for one
// System User: an ordinary user receives granted routeable nodes plus their
// active ancestors; a currently assigned active superadmin Role receives every
// active routeable node. The result preserves the repository's (parent_id,
// order_num, id) order — BuildMenuTree's stable order_num sort then retains id
// as the deterministic tie-break — and returns the menus as an ordered tree.
func SelectUserRouteTree(routeAccess *entity.MenuRouteAccess) []*entity.Menu {
	if routeAccess == nil {
		return []*entity.Menu{}
	}

	all := make(map[int64]*entity.Menu, len(routeAccess.ActiveMenus))
	for _, m := range routeAccess.ActiveMenus {
		if m != nil {
			all[m.ID] = m
		}
	}

	selected := make(map[int64]*entity.Menu, len(all))
	if hasExactRoleKey(routeAccess.RoleKeys, entity.SuperadminRoleKey) {
		for id, m := range all {
			selected[id] = m
		}
	} else {
		for _, id := range routeAccess.GrantedMenuIDs {
			m, ok := all[id]
			if !ok {
				// Disabled/deleted menus and Button-only grants do not seed a
				// route or cause their ancestors to appear.
				continue
			}
			selected[id] = m
			seenAncestors := map[int64]struct{}{m.ID: {}}
			for parentID := m.ParentID; parentID != 0; {
				if _, cycle := seenAncestors[parentID]; cycle {
					break
				}
				seenAncestors[parentID] = struct{}{}
				parent, ok := all[parentID]
				if !ok {
					// Preserve the menu-tree rule: descendants of a
					// missing/soft-deleted parent remain orphans and are
					// dropped.
					break
				}
				selected[parent.ID] = parent
				parentID = parent.ParentID
			}
		}
	}

	menus := make([]*entity.Menu, 0, len(selected))
	for _, m := range routeAccess.ActiveMenus {
		if m != nil {
			if _, ok := selected[m.ID]; ok {
				menus = append(menus, m)
			}
		}
	}
	return BuildMenuTree(menus)
}

func hasExactRoleKey(roleKeys []string, want string) bool {
	for _, roleKey := range roleKeys {
		if roleKey == want {
			return true
		}
	}
	return false
}
