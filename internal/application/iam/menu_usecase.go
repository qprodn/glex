package iam

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	iamdomain "glex/internal/domain/iam"
)

// MenuUsecase holds the menu business rules.
type MenuUsecase struct {
	repo    iamdomain.MenuRepo
	tx      Transaction
	revoker iamdomain.SessionRevoker
	logger  *slog.Logger
}

// NewMenuUsecase creates a MenuUsecase. tx wraps security writes together
// with their session revocation; revoker performs the revocation.
func NewMenuUsecase(repo iamdomain.MenuRepo, tx Transaction, revoker iamdomain.SessionRevoker, logger *slog.Logger) *MenuUsecase {
	return &MenuUsecase{
		repo:    repo,
		tx:      tx,
		revoker: revoker,
		logger:  logger.With(slog.String("component", "application.iam.menu")),
	}
}

// CreateMenu validates the type and sibling-name uniqueness, then persists.
// Uniqueness is defense-in-depth at the domain layer; there is no DB unique
// constraint on (parent_id, menu_name).
func (uc *MenuUsecase) CreateMenu(ctx context.Context, m *iamdomain.Menu) (*iamdomain.Menu, error) {
	if err := validateMenuType(m.MenuType); err != nil {
		return nil, err
	}
	if err := uc.assertSiblingUnique(ctx, m, false); err != nil {
		return nil, err
	}
	var created *iamdomain.Menu
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = uc.repo.CreateMenu(ctx, m)
		return err
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// GetMenu returns the menu with the given id.
func (uc *MenuUsecase) GetMenu(ctx context.Context, id int64) (*iamdomain.Menu, error) {
	return uc.repo.FindByID(ctx, id)
}

// ListMenuTree loads the flat list and builds the menu tree in memory.
func (uc *MenuUsecase) ListMenuTree(ctx context.Context, opts ...iamdomain.ListOption) ([]*iamdomain.Menu, error) {
	menus, err := uc.repo.ListMenus(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return buildTree(menus), nil
}

// ListUserRouteTree builds the current System User's routes from fresh
// Role-Assignment/iamdomain.Menu-Grant data. An ordinary user receives granted
// routeable nodes plus their active ancestors. A currently assigned active
// superadmin Role receives every active routeable node. The shared buildTree
// helper then applies the existing sibling ordering and orphan behavior.
func (uc *MenuUsecase) ListUserRouteTree(ctx context.Context, systemUserID int64) ([]*iamdomain.Menu, error) {
	access, err := uc.repo.LoadRouteAccess(ctx, systemUserID)
	if err != nil {
		return nil, err
	}
	if access == nil {
		return []*iamdomain.Menu{}, nil
	}

	all := make(map[int64]*iamdomain.Menu, len(access.ActiveMenus))
	for _, m := range access.ActiveMenus {
		if m != nil {
			all[m.ID] = m
		}
	}

	selected := make(map[int64]*iamdomain.Menu, len(all))
	if hasExactRoleKey(access.RoleKeys, "superadmin") {
		for id, m := range all {
			selected[id] = m
		}
	} else {
		for _, id := range access.GrantedMenuIDs {
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
					// Preserve the existing menu-tree rule: descendants of a
					// missing/soft-deleted parent remain orphans and are dropped.
					break
				}
				selected[parent.ID] = parent
				parentID = parent.ParentID
			}
		}
	}

	// Preserve the repository's (parent_id, order_num, id) order. buildTree's
	// stable order_num sort then retains id as the deterministic tie-break.
	menus := make([]*iamdomain.Menu, 0, len(selected))
	for _, m := range access.ActiveMenus {
		if m != nil {
			if _, ok := selected[m.ID]; ok {
				menus = append(menus, m)
			}
		}
	}
	return buildTree(menus), nil
}

func hasExactRoleKey(roleKeys []string, want string) bool {
	for _, roleKey := range roleKeys {
		if roleKey == want {
			return true
		}
	}
	return false
}

// UpdateMenu rejects a self-parent, re-validates the type (so an explicit mask
// cannot zero it), and enforces sibling-name uniqueness excluding self, then
// persists. When the write invalidates Authorization Snapshots
// (perms/status), affected users' Login Sessions are revoked inside the same
// transaction. That failure behavior is an implementation detail for this
// operation rather than a stable business guarantee.
func (uc *MenuUsecase) UpdateMenu(ctx context.Context, m *iamdomain.Menu) (*iamdomain.Menu, error) {
	if m.ParentID == m.ID {
		return nil, iamdomain.ErrMenuParentInvalid
	}
	if err := validateMenuType(m.MenuType); err != nil {
		return nil, err
	}
	if err := uc.assertSiblingUnique(ctx, m, true); err != nil {
		return nil, err
	}
	var updated *iamdomain.Menu
	if err := uc.tx.InTx(ctx, func(ctx context.Context) error {
		result, err := uc.repo.UpdateMenu(ctx, m)
		if err != nil {
			return err
		}
		updated = result.Menu
		if result.Menu.AuthorizationChangedFrom(result.Previous) {
			return revokeAllSystemUsers(ctx, uc.revoker, result.AffectedUserIDs...)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteMenu refuses to delete a menu that still has children, then
// soft-deletes it, removes its role links, and revokes affected users' Login
// Sessions in one transaction.
func (uc *MenuUsecase) DeleteMenu(ctx context.Context, id int64) error {
	has, err := uc.repo.HasChildren(ctx, id)
	if err != nil {
		return err
	}
	if has {
		return iamdomain.ErrMenuHasChildren
	}
	return uc.tx.InTx(ctx, func(ctx context.Context) error {
		affected, err := uc.repo.DeleteMenu(ctx, id)
		if err != nil {
			return err
		}
		return revokeAllSystemUsers(ctx, uc.revoker, affected...)
	})
}

// assertSiblingUnique checks no other menu shares (parent_id, menu_name). When
// excludeSelf is set (update), a match bearing the same id is allowed. A
// not-found from the repo is the expected "available" case.
func (uc *MenuUsecase) assertSiblingUnique(ctx context.Context, m *iamdomain.Menu, excludeSelf bool) error {
	existing, err := uc.repo.FindByParentAndName(ctx, m.ParentID, m.MenuName)
	if err != nil {
		if errors.Is(err, iamdomain.ErrMenuNotFound) {
			return nil
		}
		return err
	}
	if excludeSelf && existing.ID == m.ID {
		return nil
	}
	return iamdomain.ErrMenuNameConflict
}

// validateMenuType rejects anything outside the MenuType enum range.
func validateMenuType(t int32) error {
	if t < iamdomain.MenuTypeDirectory || t > iamdomain.MenuTypeButton {
		return iamdomain.ErrMenuTypeInvalid
	}
	return nil
}

// buildTree assembles a flat list into a forest in a single pass: roots are
// nodes whose parent_id == 0; each node pulls its children from a map keyed by
// parent id; sibling groups are sorted by order_num. Orphans — whose parent is
// missing (e.g. soft-deleted) — are dropped from the tree (they remain in the
// store and are still reachable by id).
func buildTree(menus []*iamdomain.Menu) []*iamdomain.Menu {
	byParent := make(map[int64][]*iamdomain.Menu)
	for _, m := range menus {
		byParent[m.ParentID] = append(byParent[m.ParentID], m)
	}
	for _, group := range byParent {
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].OrderNum < group[j].OrderNum
		})
	}
	roots := byParent[0]
	var attach func(nodes []*iamdomain.Menu)
	attach = func(nodes []*iamdomain.Menu) {
		for _, n := range nodes {
			n.Children = byParent[n.ID]
			attach(n.Children)
		}
	}
	attach(roots)
	return roots
}
