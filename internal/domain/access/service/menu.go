// Package service holds the access module's own rules and intra-domain
// orchestration: policies that need no other module and no transaction
// boundary. Cross-domain orchestration (transactions, session revocation)
// stays with the Application services.
package service

import (
	"context"
	"errors"
	"sort"

	"glex/internal/domain/access"
	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"
)

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
func SelectUserRouteTree(routeAccess *repository.MenuRouteAccess) []*entity.Menu {
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
