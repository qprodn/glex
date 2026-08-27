package access

import (
	. "glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
	accessrepo "glex/internal/domain/access/repository"
	accessservice "glex/internal/domain/access/service"

	accessv1 "glex/api/access/v1"

	"context"
	"testing"

	"github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fakeMenuRepo is an in-memory accessrepo.MenuRepo for testing the application
// service in isolation from the infrastructure layer.
type fakeMenuRepo struct {
	byID        map[int64]*accessentity.Menu
	next        int64
	deleted     []int64
	routeAccess *accessrepo.MenuRouteAccess
	// affectedUsers is the affected-user set the (real) repo would discover
	// under the authorization locks.
	affectedUsers []int64
}

func newFakeMenuRepo() *fakeMenuRepo {
	return &fakeMenuRepo{byID: make(map[int64]*accessentity.Menu), next: 1}
}

func (r *fakeMenuRepo) FindByID(_ context.Context, id int64) (*accessentity.Menu, error) {
	m, ok := r.byID[id]
	if !ok {
		return nil, ErrMenuNotFound
	}
	return m, nil
}

func (r *fakeMenuRepo) FindByParentAndName(_ context.Context, parentID int64, name string) (*accessentity.Menu, error) {
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

func (r *fakeMenuRepo) ListByIDs(_ context.Context, ids []int64) ([]*accessentity.Menu, error) {
	out := make([]*accessentity.Menu, 0, len(ids))
	for _, id := range ids {
		if m, ok := r.byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r *fakeMenuRepo) ListMenus(context.Context, ...accessrepo.ListOption) ([]*accessentity.Menu, error) {
	out := make([]*accessentity.Menu, 0, len(r.byID))
	for _, m := range r.byID {
		out = append(out, m)
	}
	return out, nil
}

func (r *fakeMenuRepo) LoadRouteAccess(context.Context, int64) (*accessrepo.MenuRouteAccess, error) {
	return r.routeAccess, nil
}

func (r *fakeMenuRepo) CreateMenu(_ context.Context, m *accessentity.Menu) (*accessentity.Menu, error) {
	m.ID = r.next
	r.next++
	r.byID[m.ID] = m
	return m, nil
}

func (r *fakeMenuRepo) UpdateMenu(_ context.Context, m *accessentity.Menu) (*accessrepo.MenuUpdate, error) {
	previous := r.byID[m.ID]
	r.byID[m.ID] = m
	return &accessrepo.MenuUpdate{Menu: m, Previous: previous, AffectedUserIDs: r.affectedUsers}, nil
}

func (r *fakeMenuRepo) DeleteMenu(_ context.Context, id int64) ([]int64, error) {
	r.deleted = append(r.deleted, id)
	delete(r.byID, id)
	return append([]int64(nil), r.affectedUsers...), nil
}

// treeIDs flattens a forest into the set of node ids it contains.
func treeIDs(roots []*accessentity.Menu) []int64 {
	var ids []int64
	var walk func(nodes []*accessentity.Menu)
	walk = func(nodes []*accessentity.Menu) {
		for _, n := range nodes {
			ids = append(ids, n.ID)
			walk(n.Children)
		}
	}
	walk(roots)
	return ids
}

func newMenuApp(repo *fakeMenuRepo, revoker *fakeSessionRevoker) *MenuApplication {
	return NewMenuApplication(repo, newFakeTransaction(), revoker, testLogger())
}

func createMenuRequest(m *accessv1.Menu) *accessv1.CreateMenuRequest {
	return &accessv1.CreateMenuRequest{Menu: m}
}

func TestCreateMenu_TypeRequired(t *testing.T) {
	app := newMenuApp(newFakeMenuRepo(), new(fakeSessionRevoker))

	_, err := app.CreateMenu(context.Background(), createMenuRequest(&accessv1.Menu{MenuName: "sys", MenuType: 0}))
	if !errors.Is(err, ErrMenuTypeInvalid) {
		t.Fatalf("expected ErrMenuTypeInvalid, got %v", err)
	}
}

func TestCreateMenu_SiblingNameConflict(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &accessentity.Menu{ID: 1, MenuName: "sys", ParentID: 0, MenuType: accessentity.MenuTypeDirectory}
	repo.next = 2
	app := newMenuApp(repo, new(fakeSessionRevoker))

	_, err := app.CreateMenu(context.Background(), createMenuRequest(&accessv1.Menu{MenuName: "sys", ParentId: 0, MenuType: accessv1.MenuType_MENU_TYPE_DIRECTORY}))
	if !errors.Is(err, ErrMenuNameConflict) {
		t.Fatalf("expected ErrMenuNameConflict, got %v", err)
	}
}

func TestCreateMenu_Success(t *testing.T) {
	app := newMenuApp(newFakeMenuRepo(), new(fakeSessionRevoker))

	got, err := app.CreateMenu(context.Background(), createMenuRequest(&accessv1.Menu{MenuName: "sys", ParentId: 0, MenuType: accessv1.MenuType_MENU_TYPE_DIRECTORY, OrderNum: 1}))
	if err != nil {
		t.Fatalf("create menu: %v", err)
	}
	if got.GetMenuId() == 0 {
		t.Fatal("expected non-zero id assigned by the repo")
	}
}

func TestBuildTree_NestingOrderOrphan(t *testing.T) {
	menus := []*accessentity.Menu{
		{ID: 1, ParentID: 0, OrderNum: 2, MenuName: "root1"},
		{ID: 2, ParentID: 0, OrderNum: 1, MenuName: "root2"},
		{ID: 3, ParentID: 1, OrderNum: 1, MenuName: "child"},
		{ID: 4, ParentID: 3, OrderNum: 1, MenuName: "grandchild"},
		{ID: 5, ParentID: 999, OrderNum: 1, MenuName: "orphan"}, // parent missing
	}
	roots := accessservice.BuildMenuTree(menus)

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

func TestListMenus_BuildsTreeFromFlatRepo(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &accessentity.Menu{ID: 1, ParentID: 0, OrderNum: 1, MenuName: "root", MenuType: accessentity.MenuTypeDirectory}
	repo.byID[2] = &accessentity.Menu{ID: 2, ParentID: 1, OrderNum: 1, MenuName: "child", MenuType: accessentity.MenuTypeMenu}
	app := newMenuApp(repo, new(fakeSessionRevoker))

	tree, err := app.ListMenus(context.Background(), &accessv1.ListMenusRequest{})
	if err != nil {
		t.Fatalf("list tree: %v", err)
	}
	if len(tree.GetMenus()) != 1 || tree.GetMenus()[0].GetMenuId() != 1 {
		t.Fatalf("expected single root id 1, got %+v", tree.GetMenus())
	}
	children := tree.GetMenus()[0].GetChildren()
	if len(children) != 1 || children[0].GetMenuId() != 2 {
		t.Fatalf("expected child id 2 under root, got %+v", children)
	}
}

func TestSelectUserRouteTree_GrantedNodesIncludeActiveAncestors(t *testing.T) {
	routeAccess := &accessrepo.MenuRouteAccess{
		RoleKeys:       []string{"operator"},
		GrantedMenuIDs: []int64{3, 4, 6},
		ActiveMenus: []*accessentity.Menu{
			{ID: 1, ParentID: 0, OrderNum: 2, MenuName: "System", MenuType: accessentity.MenuTypeDirectory},
			{ID: 2, ParentID: 1, OrderNum: 2, MenuName: "Users", MenuType: accessentity.MenuTypeDirectory},
			{ID: 3, ParentID: 2, OrderNum: 1, MenuName: "User list", MenuType: accessentity.MenuTypeMenu},
			{ID: 4, ParentID: 1, OrderNum: 1, MenuName: "Roles", MenuType: accessentity.MenuTypeMenu},
			{ID: 5, ParentID: 0, OrderNum: 1, MenuName: "Ungrant", MenuType: accessentity.MenuTypeMenu},
			// A missing/soft-deleted parent keeps this granted node orphaned.
			{ID: 6, ParentID: 99, OrderNum: 1, MenuName: "Orphan", MenuType: accessentity.MenuTypeMenu},
		},
	}

	ids := treeIDs(accessservice.SelectUserRouteTree(routeAccess))
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
	routeAccess := &accessrepo.MenuRouteAccess{
		RoleKeys:       []string{"operator"},
		GrantedMenuIDs: []int64{8, 9}, // absent: Button and inactive Menu
		ActiveMenus: []*accessentity.Menu{
			{ID: 1, ParentID: 0, MenuName: "System", MenuType: accessentity.MenuTypeDirectory},
		},
	}

	if roots := accessservice.SelectUserRouteTree(routeAccess); len(roots) != 0 {
		t.Fatalf("button/inactive grants produced routes: %v", treeIDs(roots))
	}
}

func TestSelectUserRouteTree_SuperadminGetsAllActiveRouteableNodes(t *testing.T) {
	routeAccess := &accessrepo.MenuRouteAccess{
		RoleKeys:       []string{"operator", "superadmin"},
		GrantedMenuIDs: nil,
		ActiveMenus: []*accessentity.Menu{
			{ID: 1, ParentID: 0, OrderNum: 2, MenuName: "Second", MenuType: accessentity.MenuTypeMenu},
			{ID: 2, ParentID: 0, OrderNum: 1, MenuName: "First", MenuType: accessentity.MenuTypeDirectory},
			{ID: 3, ParentID: 2, OrderNum: 1, MenuName: "Child", MenuType: accessentity.MenuTypeMenu},
		},
	}

	ids := treeIDs(accessservice.SelectUserRouteTree(routeAccess))
	want := []int64{2, 3, 1}
	for i := range want {
		if len(ids) != len(want) || ids[i] != want[i] {
			t.Fatalf("superadmin route ids = %v, want %v", ids, want)
		}
	}
}

func TestSelectUserRouteTree_MalformedSuperadminRoleKeyDoesNotBypassGrants(t *testing.T) {
	routeAccess := &accessrepo.MenuRouteAccess{
		RoleKeys: []string{" superadmin "},
		ActiveMenus: []*accessentity.Menu{
			{ID: 1, ParentID: 0, MenuName: "System", MenuType: accessentity.MenuTypeDirectory},
		},
	}

	if roots := accessservice.SelectUserRouteTree(routeAccess); len(roots) != 0 {
		t.Fatalf("malformed superadmin role key bypassed Menu Grants: %v", treeIDs(roots))
	}
}

func TestSelectUserRouteTree_PreservesRepositoryTieBreakForEqualOrder(t *testing.T) {
	routeAccess := &accessrepo.MenuRouteAccess{
		RoleKeys:       []string{"operator"},
		GrantedMenuIDs: []int64{2, 3},
		// The data repo supplies id order as the tie-break for equal order_num.
		ActiveMenus: []*accessentity.Menu{
			{ID: 1, ParentID: 0, OrderNum: 1, MenuType: accessentity.MenuTypeDirectory},
			{ID: 2, ParentID: 1, OrderNum: 5, MenuType: accessentity.MenuTypeMenu},
			{ID: 3, ParentID: 1, OrderNum: 5, MenuType: accessentity.MenuTypeMenu},
		},
	}

	roots := accessservice.SelectUserRouteTree(routeAccess)
	if len(roots) != 1 || len(roots[0].Children) != 2 || roots[0].Children[0].ID != 2 || roots[0].Children[1].ID != 3 {
		t.Fatalf("equal-order routes lost repository tie-break: %v", treeIDs(roots))
	}
}

func TestUpdateMenu_ParentSelfRejected(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &accessentity.Menu{ID: 1, MenuName: "sys", ParentID: 0, MenuType: accessentity.MenuTypeDirectory}
	app := newMenuApp(repo, new(fakeSessionRevoker))

	_, err := app.UpdateMenu(context.Background(), &accessv1.UpdateMenuRequest{Menu: &accessv1.Menu{MenuId: 1, MenuName: "sys", ParentId: 1, MenuType: accessv1.MenuType_MENU_TYPE_DIRECTORY}})
	if !errors.Is(err, ErrMenuParentInvalid) {
		t.Fatalf("expected ErrMenuParentInvalid, got %v", err)
	}
}

func TestUpdateMenu_TypeInvalid(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &accessentity.Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: accessentity.MenuTypeDirectory}
	app := newMenuApp(repo, new(fakeSessionRevoker))

	// An explicit mask cannot zero menu_type.
	_, err := app.UpdateMenu(context.Background(), &accessv1.UpdateMenuRequest{
		Menu:       &accessv1.Menu{MenuId: 1, MenuName: "a", ParentId: 0, MenuType: 0},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"menu_name", "parent_id", "menu_type"}},
	})
	if !errors.Is(err, ErrMenuTypeInvalid) {
		t.Fatalf("expected ErrMenuTypeInvalid, got %v", err)
	}
}

func TestUpdateMenu_SiblingNameConflictExcludingSelf(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &accessentity.Menu{ID: 1, MenuName: "a", ParentID: 0, MenuType: accessentity.MenuTypeDirectory}
	repo.byID[2] = &accessentity.Menu{ID: 2, MenuName: "b", ParentID: 0, MenuType: accessentity.MenuTypeDirectory}
	app := newMenuApp(repo, new(fakeSessionRevoker))

	// Renaming to its own name (unchanged) is allowed: the match is self.
	if _, err := app.UpdateMenu(context.Background(), &accessv1.UpdateMenuRequest{Menu: &accessv1.Menu{MenuId: 1, MenuName: "a", MenuType: accessv1.MenuType_MENU_TYPE_DIRECTORY}}); err != nil {
		t.Fatalf("update to own name should succeed, got %v", err)
	}
	// Renaming to a sibling's name conflicts.
	_, err := app.UpdateMenu(context.Background(), &accessv1.UpdateMenuRequest{Menu: &accessv1.Menu{MenuId: 1, MenuName: "b", MenuType: accessv1.MenuType_MENU_TYPE_DIRECTORY}})
	if !errors.Is(err, ErrMenuNameConflict) {
		t.Fatalf("expected ErrMenuNameConflict, got %v", err)
	}
}

func TestDeleteMenu_HasChildrenRejected(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[1] = &accessentity.Menu{ID: 1, ParentID: 0, MenuType: accessentity.MenuTypeDirectory}
	repo.byID[2] = &accessentity.Menu{ID: 2, ParentID: 1, MenuType: accessentity.MenuTypeMenu}
	app := newMenuApp(repo, new(fakeSessionRevoker))

	if _, err := app.DeleteMenu(context.Background(), &accessv1.DeleteMenuRequest{MenuId: 1}); !errors.Is(err, ErrMenuHasChildren) {
		t.Fatalf("expected ErrMenuHasChildren, got %v", err)
	}
}

func TestDeleteMenu_LeafSuccess(t *testing.T) {
	repo := newFakeMenuRepo()
	repo.byID[2] = &accessentity.Menu{ID: 2, ParentID: 1, MenuType: accessentity.MenuTypeMenu}
	app := newMenuApp(repo, new(fakeSessionRevoker))

	if _, err := app.DeleteMenu(context.Background(), &accessv1.DeleteMenuRequest{MenuId: 2}); err != nil {
		t.Fatalf("delete leaf: %v", err)
	}
	if _, ok := repo.byID[2]; ok {
		t.Fatal("expected id 2 removed from repo")
	}
}
