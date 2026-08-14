package biz

import (
	"context"
	"errors"
	"sort"
	"time"
)

// MenuType values for Menu.MenuType (mirror of the api/system/menu/v1 MenuType
// enum). The DO carries the int32 so biz stays free of proto types.
const (
	MenuTypeDirectory int32 = 1 // 目录
	MenuTypeMenu      int32 = 2 // 菜单
	MenuTypeButton    int32 = 3 // 按钮
)

// Menu is a menu-node domain object (菜单), ported from RuoYi sys_menu. Pure biz
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

// MenuRepo is the repository interface for menus (the inversion seam declared
// in biz, implemented in data).
type MenuRepo interface {
	FindByID(ctx context.Context, id int64) (*Menu, error)
	FindByParentAndName(ctx context.Context, parentID int64, name string) (*Menu, error)
	HasChildren(ctx context.Context, id int64) (bool, error)
	// ListByIDs returns the menus with the given ids that exist and are not
	// soft-deleted (used to validate Menu Grants).
	ListByIDs(ctx context.Context, ids []int64) ([]*Menu, error)
	ListMenus(ctx context.Context, opts ...ListOption) ([]*Menu, error)
	CreateMenu(ctx context.Context, m *Menu) (*Menu, error)
	UpdateMenu(ctx context.Context, m *Menu) (*Menu, error)
	DeleteMenu(ctx context.Context, id int64) error
}

// MenuUsecase holds the menu business rules.
type MenuUsecase struct {
	repo MenuRepo
}

// NewMenuUsecase creates a MenuUsecase.
func NewMenuUsecase(repo MenuRepo) *MenuUsecase {
	return &MenuUsecase{repo: repo}
}

// CreateMenu validates the type and sibling-name uniqueness, then persists.
// Uniqueness is defense-in-depth at the biz layer; there is no DB unique
// constraint on (parent_id, menu_name).
func (uc *MenuUsecase) CreateMenu(ctx context.Context, m *Menu) (*Menu, error) {
	if err := validateMenuType(m.MenuType); err != nil {
		return nil, err
	}
	if err := uc.assertSiblingUnique(ctx, m, false); err != nil {
		return nil, err
	}
	return uc.repo.CreateMenu(ctx, m)
}

// GetMenu returns the menu with the given id.
func (uc *MenuUsecase) GetMenu(ctx context.Context, id int64) (*Menu, error) {
	return uc.repo.FindByID(ctx, id)
}

// ListMenuTree loads the flat list and builds the menu tree in memory.
func (uc *MenuUsecase) ListMenuTree(ctx context.Context, opts ...ListOption) ([]*Menu, error) {
	menus, err := uc.repo.ListMenus(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return buildTree(menus), nil
}

// UpdateMenu rejects a self-parent, re-validates the type (so an explicit mask
// cannot zero it), and enforces sibling-name uniqueness excluding self, then
// persists.
func (uc *MenuUsecase) UpdateMenu(ctx context.Context, m *Menu) (*Menu, error) {
	if m.ParentID == m.ID {
		return nil, ErrMenuParentInvalid
	}
	if err := validateMenuType(m.MenuType); err != nil {
		return nil, err
	}
	if err := uc.assertSiblingUnique(ctx, m, true); err != nil {
		return nil, err
	}
	return uc.repo.UpdateMenu(ctx, m)
}

// DeleteMenu refuses to delete a menu that still has children, then soft-deletes.
func (uc *MenuUsecase) DeleteMenu(ctx context.Context, id int64) error {
	has, err := uc.repo.HasChildren(ctx, id)
	if err != nil {
		return err
	}
	if has {
		return ErrMenuHasChildren
	}
	return uc.repo.DeleteMenu(ctx, id)
}

// assertSiblingUnique checks no other menu shares (parent_id, menu_name). When
// excludeSelf is set (update), a match bearing the same id is allowed. A
// not-found from the repo is the expected "available" case.
func (uc *MenuUsecase) assertSiblingUnique(ctx context.Context, m *Menu, excludeSelf bool) error {
	existing, err := uc.repo.FindByParentAndName(ctx, m.ParentID, m.MenuName)
	if err != nil {
		if errors.Is(err, ErrMenuNotFound) {
			return nil
		}
		return err
	}
	if excludeSelf && existing.ID == m.ID {
		return nil
	}
	return ErrMenuNameConflict
}

// validateMenuType rejects anything outside the MenuType enum range.
func validateMenuType(t int32) error {
	if t < MenuTypeDirectory || t > MenuTypeButton {
		return ErrMenuTypeInvalid
	}
	return nil
}

// buildTree assembles a flat list into a forest in a single pass: roots are
// nodes whose parent_id == 0; each node pulls its children from a map keyed by
// parent id; sibling groups are sorted by order_num. Orphans — whose parent is
// missing (e.g. soft-deleted) — are dropped from the tree (they remain in the
// store and are still reachable by id).
func buildTree(menus []*Menu) []*Menu {
	byParent := make(map[int64][]*Menu)
	for _, m := range menus {
		byParent[m.ParentID] = append(byParent[m.ParentID], m)
	}
	for _, group := range byParent {
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].OrderNum < group[j].OrderNum
		})
	}
	roots := byParent[0]
	var attach func(nodes []*Menu)
	attach = func(nodes []*Menu) {
		for _, n := range nodes {
			n.Children = byParent[n.ID]
			attach(n.Children)
		}
	}
	attach(roots)
	return roots
}
