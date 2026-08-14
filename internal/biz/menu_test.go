package biz

import (
	"context"
	"testing"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeMenuRepo is an in-memory MenuRepo for testing the usecase in isolation
// from the data layer.
type fakeMenuRepo struct {
	byID    map[int64]*Menu
	next    int64
	deleted []int64
}

func newFakeMenuRepo() *fakeMenuRepo {
	return &fakeMenuRepo{byID: make(map[int64]*Menu), next: 1}
}

func (r *fakeMenuRepo) FindByID(_ context.Context, id int64) (*Menu, error) {
	m, ok := r.byID[id]
	if !ok {
		return nil, ErrMenuNotFound
	}
	return m, nil
}

func (r *fakeMenuRepo) FindByParentAndName(_ context.Context, parentID int64, name string) (*Menu, error) {
	for _, m := range r.byID {
		if m.ParentID == parentID && m.MenuName == name {
			return m, nil
		}
	}
	return nil, ErrMenuNotFound
}

func (r *fakeMenuRepo) HasChildren(_ context.Context, id int64) (bool, error) {
	for _, m := range r.byID {
		if m.ParentID == id {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeMenuRepo) ListByIDs(_ context.Context, ids []int64) ([]*Menu, error) {
	out := make([]*Menu, 0, len(ids))
	for _, id := range ids {
		if m, ok := r.byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r *fakeMenuRepo) ListMenus(context.Context, ...ListOption) ([]*Menu, error) {
	out := make([]*Menu, 0, len(r.byID))
	for _, m := range r.byID {
		out = append(out, m)
	}
	return out, nil
}

func (r *fakeMenuRepo) CreateMenu(_ context.Context, m *Menu) (*Menu, error) {
	m.ID = r.next
	r.next++
	r.byID[m.ID] = m
	return m, nil
}

func (r *fakeMenuRepo) UpdateMenu(_ context.Context, m *Menu) (*Menu, error) {
	r.byID[m.ID] = m
	return m, nil
}

func (r *fakeMenuRepo) DeleteMenu(_ context.Context, id int64) error {
	r.deleted = append(r.deleted, id)
	delete(r.byID, id)
	return nil
}

// treeIDs flattens a forest into the set of node ids it contains.
func treeIDs(roots []*Menu) []int64 {
	var ids []int64
	var walk func(nodes []*Menu)
	walk = func(nodes []*Menu) {
		for _, n := range nodes {
			ids = append(ids, n.ID)
			walk(n.Children)
		}
	}
	walk(roots)
	return ids
}

func TestCreateMenu_TypeRequired(t *testing.T) {
	repo := newFakeMenuRepo()
	uc := NewMenuUsecase(repo)

	_, err := uc.CreateMenu(context.Background(), &Menu{MenuName: "sys", MenuType: 0})
	if !errors.Is(err, ErrMenuTypeInvalid) {
		t.Fatalf("expected ErrMenuTypeInvalid, got %v", err)
	}
}

func TestCreateMenu_SiblingNameConflict(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &Menu{ID: 1, MenuName: "sys", ParentID: 0, MenuType: MenuTypeDirectory}
	repo.next = 2
	uc := NewMenuUsecase(repo)

	_, err := uc.CreateMenu(context.Background(), &Menu{MenuName: "sys", ParentID: 0, MenuType: MenuTypeMenu})
	if !errors.Is(err, ErrMenuNameConflict) {
		t.Fatalf("expected ErrMenuNameConflict, got %v", err)
	}
}

func TestCreateMenu_Success(t *testing.T) {
	repo := newFakeMenuRepo()
	uc := NewMenuUsecase(repo)

	got, err := uc.CreateMenu(context.Background(), &Menu{MenuName: "sys", ParentID: 0, MenuType: MenuTypeDirectory, OrderNum: 1})
	if err != nil {
		t.Fatalf("create menu: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero id assigned by the repo")
	}
}

func TestBuildTree_NestingOrderOrphan(t *testing.T) {
	menus := []*Menu{
		{ID: 1, ParentID: 0, OrderNum: 2, MenuName: "root1"},
		{ID: 2, ParentID: 0, OrderNum: 1, MenuName: "root2"},
		{ID: 3, ParentID: 1, OrderNum: 1, MenuName: "child"},
		{ID: 4, ParentID: 3, OrderNum: 1, MenuName: "grandchild"},
		{ID: 5, ParentID: 999, OrderNum: 1, MenuName: "orphan"}, // parent missing
	}
	roots := buildTree(menus)

	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(roots))
	}
	// Roots sorted by OrderNum: root2 (1) before root1 (2).
	if roots[0].ID != 2 || roots[1].ID != 1 {
		t.Fatalf("root order wrong: got %d, %d", roots[0].ID, roots[1].ID)
	}
	// root1 → child → grandchild.
	if len(roots[1].Children) != 1 || roots[1].Children[0].ID != 3 {
		t.Fatalf("root1 child wrong: %+v", roots[1].Children)
	}
	if len(roots[1].Children[0].Children) != 1 || roots[1].Children[0].Children[0].ID != 4 {
		t.Fatal("grandchild wrong under root1")
	}
	// root2 has no children.
	if len(roots[0].Children) != 0 {
		t.Fatalf("root2 should have no children, got %d", len(roots[0].Children))
	}
	// Orphan (parent 999 missing) is dropped from the tree.
	ids := treeIDs(roots)
	if len(ids) != 4 {
		t.Fatalf("expected 4 nodes in tree, got %d (%v)", len(ids), ids)
	}
	for _, id := range ids {
		if id == 5 {
			t.Fatal("orphan id 5 should be dropped from the tree")
		}
	}
}

func TestListMenuTree_BuildsFromFlatRepo(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &Menu{ID: 1, ParentID: 0, OrderNum: 1, MenuName: "root", MenuType: MenuTypeDirectory}
	repo.byID[2] = &Menu{ID: 2, ParentID: 1, OrderNum: 1, MenuName: "child", MenuType: MenuTypeMenu}
	uc := NewMenuUsecase(repo)

	roots, err := uc.ListMenuTree(context.Background())
	if err != nil {
		t.Fatalf("list tree: %v", err)
	}
	if len(roots) != 1 || roots[0].ID != 1 {
		t.Fatalf("expected single root id 1, got %+v", roots)
	}
	if len(roots[0].Children) != 1 || roots[0].Children[0].ID != 2 {
		t.Fatalf("expected child id 2 under root, got %+v", roots[0].Children)
	}
}

func TestUpdateMenu_ParentSelfRejected(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &Menu{ID: 1, MenuName: "sys", ParentID: 0, MenuType: MenuTypeDirectory}
	uc := NewMenuUsecase(repo)

	_, err := uc.UpdateMenu(context.Background(), &Menu{ID: 1, MenuName: "sys", ParentID: 1, MenuType: MenuTypeDirectory})
	if !errors.Is(err, ErrMenuParentInvalid) {
		t.Fatalf("expected ErrMenuParentInvalid, got %v", err)
	}
}

func TestUpdateMenu_TypeInvalid(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: MenuTypeDirectory}
	uc := NewMenuUsecase(repo)

	_, err := uc.UpdateMenu(context.Background(), &Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: 0})
	if !errors.Is(err, ErrMenuTypeInvalid) {
		t.Fatalf("expected ErrMenuTypeInvalid, got %v", err)
	}
}

func TestUpdateMenu_SiblingNameConflictExcludingSelf(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: MenuTypeDirectory}
	repo.byID[2] = &Menu{ID: 2, MenuName: "b", ParentID: 0, MenuType: MenuTypeDirectory}
	uc := NewMenuUsecase(repo)

	// Renaming to its own name (unchanged) is allowed: the match is self.
	if _, err := uc.UpdateMenu(context.Background(), &Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: MenuTypeDirectory}); err != nil {
		t.Fatalf("update to own name should succeed, got %v", err)
	}
	// Renaming to a sibling's name conflicts.
	_, err := uc.UpdateMenu(context.Background(), &Menu{ID: 1, MenuName: "b", ParentID: 0, MenuType: MenuTypeDirectory})
	if !errors.Is(err, ErrMenuNameConflict) {
		t.Fatalf("expected ErrMenuNameConflict, got %v", err)
	}
}

func TestDeleteMenu_HasChildrenRejected(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &Menu{ID: 1, ParentID: 0, MenuType: MenuTypeDirectory}
	repo.byID[2] = &Menu{ID: 2, ParentID: 1, MenuType: MenuTypeMenu}
	uc := NewMenuUsecase(repo)

	if err := uc.DeleteMenu(context.Background(), 1); !errors.Is(err, ErrMenuHasChildren) {
		t.Fatalf("expected ErrMenuHasChildren, got %v", err)
	}
}

func TestDeleteMenu_LeafSuccess(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[2] = &Menu{ID: 2, ParentID: 1, MenuType: MenuTypeMenu}
	uc := NewMenuUsecase(repo)

	if err := uc.DeleteMenu(context.Background(), 2); err != nil {
		t.Fatalf("delete leaf: %v", err)
	}
	if _, ok := repo.byID[2]; ok {
		t.Fatal("expected id 2 removed from repo")
	}
}
