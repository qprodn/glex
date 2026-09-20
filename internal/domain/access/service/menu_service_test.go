package service

import (
	"context"
	"testing"

	"glex/internal/domain/access"
	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeMenuRepo is an in-memory repository.MenuRepo for testing the menu and
// role domain services in isolation from the infrastructure layer.
type fakeMenuRepo struct {
	byID        map[int64]*entity.Menu
	next        int64
	deleted     []int64
	routeAccess *entity.MenuRouteAccess
	// affectedUsers is the affected-user set the (real) repo would discover
	// under the authorization locks.
	affectedUsers []int64
}

func newFakeMenuRepo() *fakeMenuRepo {
	return &fakeMenuRepo{byID: make(map[int64]*entity.Menu), next: 1}
}

func (r *fakeMenuRepo) FindByID(_ context.Context, id int64) (*entity.Menu, error) {
	m, ok := r.byID[id]
	if !ok {
		return nil, access.ErrMenuNotFound
	}
	return m, nil
}

func (r *fakeMenuRepo) FindByParentAndName(_ context.Context, req repository.FindByParentAndNameRequest) (*entity.Menu, error) {
	for _, m := range r.byID {
		if m.ParentID == req.ParentID && m.MenuName == req.Name {
			return m, nil
		}
	}
	return nil, access.ErrMenuNotFound
}

func (r *fakeMenuRepo) HasChildren(_ context.Context, id int64) (bool, error) {
	for _, m := range r.byID {
		if m.ParentID == id {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeMenuRepo) ListByIDs(_ context.Context, ids []int64) ([]*entity.Menu, error) {
	out := make([]*entity.Menu, 0, len(ids))
	for _, id := range ids {
		if m, ok := r.byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r *fakeMenuRepo) ListMenus(context.Context, repository.ListMenusRequest) ([]*entity.Menu, error) {
	out := make([]*entity.Menu, 0, len(r.byID))
	for _, m := range r.byID {
		out = append(out, m)
	}
	return out, nil
}

func (r *fakeMenuRepo) LoadRouteAccess(context.Context, int64) (*entity.MenuRouteAccess, error) {
	return r.routeAccess, nil
}

func (r *fakeMenuRepo) CreateMenu(_ context.Context, m *entity.Menu) (*entity.Menu, error) {
	m.ID = r.next
	r.next++
	r.byID[m.ID] = m
	return m, nil
}

func (r *fakeMenuRepo) UpdateMenu(_ context.Context, m *entity.Menu) (*entity.MenuUpdate, error) {
	previous := r.byID[m.ID]
	r.byID[m.ID] = m
	return &entity.MenuUpdate{Menu: m, Previous: previous, AffectedUserIDs: r.affectedUsers}, nil
}

func (r *fakeMenuRepo) DeleteMenu(_ context.Context, req repository.DeleteMenuRequest) ([]int64, error) {
	r.deleted = append(r.deleted, req.ID)
	delete(r.byID, req.ID)
	return append([]int64(nil), r.affectedUsers...), nil
}

// newFakeMenuRepoForRole returns a fakeMenuRepo preloaded with the given
// menus (ids are the map keys), for the role service's Menu Grant validation.
func newFakeMenuRepoForRole(menus map[int64]*entity.Menu) *fakeMenuRepo {
	repo := newFakeMenuRepo()
	for id, m := range menus {
		m.ID = id
		repo.byID[id] = m
	}
	return repo
}

// newMenuSvc wires the real menu domain service over the fake repo.
func newMenuSvc(menus *fakeMenuRepo) Menu {
	return NewMenuService(menus, testLogger())
}

// treeIDs flattens a forest into the set of node ids it contains.
func treeIDs(roots []*entity.Menu) []int64 {
	var ids []int64
	var walk func(nodes []*entity.Menu)
	walk = func(nodes []*entity.Menu) {
		for _, n := range nodes {
			ids = append(ids, n.ID)
			walk(n.Children)
		}
	}
	walk(roots)
	return ids
}

func TestMenuCreate_Success(t *testing.T) {
	svc := newMenuSvc(newFakeMenuRepo())

	created, err := svc.Create(context.Background(), &entity.Menu{MenuName: "sys", ParentID: 0, MenuType: entity.MenuTypeDirectory, OrderNum: 1})
	if err != nil {
		t.Fatalf("create menu: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero id assigned by the repo")
	}
}

func TestMenuCreate_TypeRequired(t *testing.T) {
	svc := newMenuSvc(newFakeMenuRepo())

	_, err := svc.Create(context.Background(), &entity.Menu{MenuName: "sys", MenuType: 0})
	if !errors.Is(err, access.ErrMenuTypeInvalid) {
		t.Fatalf("expected ErrMenuTypeInvalid, got %v", err)
	}
}

func TestMenuCreate_SiblingNameConflict(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &entity.Menu{ID: 1, MenuName: "sys", ParentID: 0, MenuType: entity.MenuTypeDirectory}
	repo.next = 2
	svc := newMenuSvc(repo)

	_, err := svc.Create(context.Background(), &entity.Menu{MenuName: "sys", ParentID: 0, MenuType: entity.MenuTypeDirectory})
	if !errors.Is(err, access.ErrMenuNameConflict) {
		t.Fatalf("expected ErrMenuNameConflict, got %v", err)
	}
}

func TestMenuUpdate_ParentSelfRejected(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &entity.Menu{ID: 1, MenuName: "sys", ParentID: 0, MenuType: entity.MenuTypeDirectory}
	svc := newMenuSvc(repo)

	_, err := svc.Update(context.Background(), &entity.Menu{ID: 1, MenuName: "sys", ParentID: 1, MenuType: entity.MenuTypeDirectory})
	if !errors.Is(err, access.ErrMenuParentInvalid) {
		t.Fatalf("expected ErrMenuParentInvalid, got %v", err)
	}
}

func TestMenuUpdate_TypeInvalid(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &entity.Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: entity.MenuTypeDirectory}
	svc := newMenuSvc(repo)

	// Any persisted change re-validates the type, so it cannot be zeroed.
	_, err := svc.Update(context.Background(), &entity.Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: 0})
	if !errors.Is(err, access.ErrMenuTypeInvalid) {
		t.Fatalf("expected ErrMenuTypeInvalid, got %v", err)
	}
}

func TestMenuUpdate_SiblingNameConflictExcludingSelf(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &entity.Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: entity.MenuTypeDirectory}
	repo.byID[2] = &entity.Menu{ID: 2, MenuName: "b", ParentID: 0, MenuType: entity.MenuTypeDirectory}
	svc := newMenuSvc(repo)

	// Renaming to its own name (unchanged) is allowed: the match is self.
	if _, err := svc.Update(context.Background(), &entity.Menu{ID: 1, MenuName: "a", MenuType: entity.MenuTypeDirectory}); err != nil {
		t.Fatalf("update to own name should succeed, got %v", err)
	}
	// Renaming to a sibling's name conflicts.
	_, err := svc.Update(context.Background(), &entity.Menu{ID: 1, MenuName: "b", MenuType: entity.MenuTypeDirectory})
	if !errors.Is(err, access.ErrMenuNameConflict) {
		t.Fatalf("expected ErrMenuNameConflict, got %v", err)
	}
}

func TestMenuDelete_HasChildrenRejected(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &entity.Menu{ID: 1, ParentID: 0, MenuType: entity.MenuTypeDirectory}
	repo.byID[2] = &entity.Menu{ID: 2, ParentID: 1, MenuType: entity.MenuTypeMenu}
	svc := newMenuSvc(repo)

	if _, err := svc.Delete(context.Background(), repository.DeleteMenuRequest{ID: 1}); !errors.Is(err, access.ErrMenuHasChildren) {
		t.Fatalf("expected ErrMenuHasChildren, got %v", err)
	}
}

func TestMenuDelete_LeafSuccess(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[2] = &entity.Menu{ID: 2, ParentID: 1, MenuType: entity.MenuTypeMenu}
	svc := newMenuSvc(repo)

	if _, err := svc.Delete(context.Background(), repository.DeleteMenuRequest{ID: 2}); err != nil {
		t.Fatalf("delete leaf: %v", err)
	}
	if _, ok := repo.byID[2]; ok {
		t.Fatal("expected id 2 removed from repo")
	}
}

// --- pure shaping functions ---

func TestBuildTree_NestingOrderOrphan(t *testing.T) {
	menus := []*entity.Menu{
		{ID: 1, ParentID: 0, OrderNum: 2, MenuName: "root1"},
		{ID: 2, ParentID: 0, OrderNum: 1, MenuName: "root2"},
		{ID: 3, ParentID: 1, OrderNum: 1, MenuName: "child"},
		{ID: 4, ParentID: 3, OrderNum: 1, MenuName: "grandchild"},
		{ID: 5, ParentID: 999, OrderNum: 1, MenuName: "orphan"}, // parent missing
	}
	roots := BuildMenuTree(menus)

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

func TestSelectUserRouteTree_GrantedNodesIncludeActiveAncestors(t *testing.T) {
	routeAccess := &entity.MenuRouteAccess{
		RoleKeys:       []string{"operator"},
		GrantedMenuIDs: []int64{3, 4, 6},
		ActiveMenus: []*entity.Menu{
			{ID: 1, ParentID: 0, OrderNum: 2, MenuName: "System", MenuType: entity.MenuTypeDirectory},
			{ID: 2, ParentID: 1, OrderNum: 2, MenuName: "Users", MenuType: entity.MenuTypeDirectory},
			{ID: 3, ParentID: 2, OrderNum: 1, MenuName: "User list", MenuType: entity.MenuTypeMenu},
			{ID: 4, ParentID: 1, OrderNum: 1, MenuName: "Roles", MenuType: entity.MenuTypeMenu},
			{ID: 5, ParentID: 0, OrderNum: 1, MenuName: "Ungrant", MenuType: entity.MenuTypeMenu},
			// A missing/soft-deleted parent keeps this granted node orphaned.
			{ID: 6, ParentID: 99, OrderNum: 1, MenuName: "Orphan", MenuType: entity.MenuTypeMenu},
		},
	}

	ids := treeIDs(SelectUserRouteTree(routeAccess))
	want := []int64{1, 4, 2, 3}
	if len(ids) != len(want) {
		t.Fatalf("route ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("route ids = %v, want %v", ids, want)
		}
	}
}

func TestSelectUserRouteTree_ButtonOnlyAndInactiveGrantsProduceNoRoutes(t *testing.T) {
	routeAccess := &entity.MenuRouteAccess{
		RoleKeys:       []string{"operator"},
		GrantedMenuIDs: []int64{8, 9}, // absent: Button and inactive Menu
		ActiveMenus: []*entity.Menu{
			{ID: 1, ParentID: 0, MenuName: "System", MenuType: entity.MenuTypeDirectory},
		},
	}

	if roots := SelectUserRouteTree(routeAccess); len(roots) != 0 {
		t.Fatalf("button/inactive grants produced routes: %v", treeIDs(roots))
	}
}

func TestSelectUserRouteTree_SuperadminGetsAllActiveRouteableNodes(t *testing.T) {
	routeAccess := &entity.MenuRouteAccess{
		RoleKeys:       []string{"operator", entity.SuperadminRoleKey},
		GrantedMenuIDs: nil,
		ActiveMenus: []*entity.Menu{
			{ID: 1, ParentID: 0, OrderNum: 2, MenuName: "Second", MenuType: entity.MenuTypeMenu},
			{ID: 2, ParentID: 0, OrderNum: 1, MenuName: "First", MenuType: entity.MenuTypeDirectory},
			{ID: 3, ParentID: 2, OrderNum: 1, MenuName: "Child", MenuType: entity.MenuTypeMenu},
		},
	}

	ids := treeIDs(SelectUserRouteTree(routeAccess))
	want := []int64{2, 3, 1}
	for i := range want {
		if len(ids) != len(want) || ids[i] != want[i] {
			t.Fatalf("superadmin route ids = %v, want %v", ids, want)
		}
	}
}

func TestSelectUserRouteTree_MalformedSuperadminRoleKeyDoesNotBypassGrants(t *testing.T) {
	routeAccess := &entity.MenuRouteAccess{
		RoleKeys: []string{" superadmin "},
		ActiveMenus: []*entity.Menu{
			{ID: 1, ParentID: 0, MenuName: "System", MenuType: entity.MenuTypeDirectory},
		},
	}

	if roots := SelectUserRouteTree(routeAccess); len(roots) != 0 {
		t.Fatalf("malformed superadmin role key bypassed Menu Grants: %v", treeIDs(roots))
	}
}

func TestSelectUserRouteTree_PreservesRepositoryTieBreakForEqualOrder(t *testing.T) {
	routeAccess := &entity.MenuRouteAccess{
		RoleKeys:       []string{"operator"},
		GrantedMenuIDs: []int64{2, 3},
		// The data repo supplies id order as the tie-break for equal order_num.
		ActiveMenus: []*entity.Menu{
			{ID: 1, ParentID: 0, OrderNum: 1, MenuType: entity.MenuTypeDirectory},
			{ID: 2, ParentID: 1, OrderNum: 5, MenuType: entity.MenuTypeMenu},
			{ID: 3, ParentID: 1, OrderNum: 5, MenuType: entity.MenuTypeMenu},
		},
	}

	roots := SelectUserRouteTree(routeAccess)
	if len(roots) != 1 || len(roots[0].Children) != 2 || roots[0].Children[0].ID != 2 || roots[0].Children[1].ID != 3 {
		t.Fatalf("equal-order routes lost repository tie-break: %v", treeIDs(roots))
	}
}
