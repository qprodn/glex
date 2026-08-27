package access

import (
	. "glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
	accessrepo "glex/internal/domain/access/repository"

	accessv1 "glex/api/access/v1"

	"context"
	"testing"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeRoleRepo is an in-memory accessrepo.RoleRepo for testing the application
// service in isolation from the infrastructure layer. Menu Grant state: links
// maps roleID → granted menu ids (in insertion order); menuParents supplies
// the parent ids for ListRoleMenuLinks.
type fakeRoleRepo struct {
	byID        map[int64]*accessentity.Role
	byName      map[string]*accessentity.Role
	byKey       map[string]*accessentity.Role
	nextID      int64
	created     []*accessentity.Role
	deleted     []int64
	links       map[int64][]int64
	menuParents map[int64]int64
	replaced    []int64
	// assignedUsers is the assigned-user set the (real) repo would discover
	// under the authorization locks; deleteAffected mirrors it for deletes.
	assignedUsers  []int64
	deleteAffected []int64
}

func newFakeRoleRepo() *fakeRoleRepo {
	return &fakeRoleRepo{
		byID:        make(map[int64]*accessentity.Role),
		byName:      make(map[string]*accessentity.Role),
		byKey:       make(map[string]*accessentity.Role),
		links:       make(map[int64][]int64),
		menuParents: make(map[int64]int64),
		nextID:      1,
	}
}

func (r *fakeRoleRepo) FindByID(_ context.Context, id int64) (*accessentity.Role, error) {
	ro, ok := r.byID[id]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) FindByRoleName(_ context.Context, roleName string) (*accessentity.Role, error) {
	ro, ok := r.byName[roleName]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) FindByRoleKey(_ context.Context, roleKey string) (*accessentity.Role, error) {
	ro, ok := r.byKey[roleKey]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) ListRoles(context.Context, ...accessrepo.ListOption) ([]*accessentity.Role, error) {
	return nil, nil
}

func (r *fakeRoleRepo) CreateRole(_ context.Context, ro *accessentity.Role, menuIDs []int64) (*accessentity.Role, error) {
	ro.ID = r.nextID
	r.nextID++
	r.byID[ro.ID] = ro
	r.byName[ro.RoleName] = ro
	r.byKey[ro.RoleKey] = ro
	r.created = append(r.created, ro)
	r.links[ro.ID] = append([]int64(nil), menuIDs...)
	return ro, nil
}

func (r *fakeRoleRepo) UpdateRole(_ context.Context, ro *accessentity.Role) (*accessrepo.RoleUpdate, error) {
	previous := r.byID[ro.ID]
	r.byID[ro.ID] = ro
	return &accessrepo.RoleUpdate{
		Role:            ro,
		Previous:        previous,
		AssignedUserIDs: r.assignedUsers,
	}, nil
}

func (r *fakeRoleRepo) DeleteRole(_ context.Context, id int64) ([]int64, error) {
	if _, ok := r.byID[id]; !ok {
		return nil, ErrRoleNotFound
	}
	r.deleted = append(r.deleted, id)
	if r.deleteAffected != nil {
		return r.deleteAffected, nil
	}
	return nil, nil
}

func (r *fakeRoleRepo) ListRoleMenuLinks(_ context.Context, roleID int64) ([]*accessrepo.RoleMenuLink, error) {
	ids := r.links[roleID]
	out := make([]*accessrepo.RoleMenuLink, 0, len(ids))
	for _, mid := range ids {
		out = append(out, &accessrepo.RoleMenuLink{MenuID: mid, ParentID: r.menuParents[mid]})
	}
	return out, nil
}

func (r *fakeRoleRepo) ReplaceRoleMenus(_ context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool) ([]int64, error) {
	ro, ok := r.byID[roleID]
	if !ok {
		return nil, ErrRoleNotFound
	}
	ro.MenuCheckStrictly = menuCheckStrictly
	r.links[roleID] = append([]int64(nil), menuIDs...)
	r.replaced = append(r.replaced, roleID)
	return append([]int64(nil), r.assignedUsers...), nil
}

// newFakeMenuRepoForRole returns a fakeMenuRepo preloaded with the given
// menus (ids are the map keys).
func newFakeMenuRepoForRole(menus map[int64]*accessentity.Menu) *fakeMenuRepo {
	repo := newFakeMenuRepo()
	for id, m := range menus {
		m.ID = id
		repo.byID[id] = m
	}
	return repo
}

func newRoleApp(repo *fakeRoleRepo, menus *fakeMenuRepo, link *fakeUserRoleRepo) *RoleApplication {
	return NewRoleApplication(repo, menus, link, newFakeTransaction(), new(fakeSessionRevoker), testLogger())
}

func TestCreateRole_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	got, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{
		Role:              &accessv1.Role{RoleName: "超级管理员", RoleKey: "superadmin", RoleSort: 1, MenuCheckStrictly: true},
		MenuCheckStrictly: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if got.GetRoleId() == 0 {
		t.Fatal("expected a server-assigned id, got 0")
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created role, got %d", len(repo.created))
	}
	// The DO passes through as-is; the nil→true default lives at the service
	// boundary (request presence), not in the domain.
	if !got.GetMenuCheckStrictly() {
		t.Fatal("expected menu_check_strictly to persist")
	}
}

func TestCreateRole_BlankNameRejected(t *testing.T) {
	app := newRoleApp(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{Role: &accessv1.Role{RoleName: "", RoleKey: "superadmin"}})
	if !errors.Is(err, ErrRoleInvalid) {
		t.Fatalf("expected ErrRoleInvalid, got %v", err)
	}
}

func TestCreateRole_BlankKeyRejected(t *testing.T) {
	app := newRoleApp(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{Role: &accessv1.Role{RoleName: "超级管理员", RoleKey: ""}})
	if !errors.Is(err, ErrRoleInvalid) {
		t.Fatalf("expected ErrRoleInvalid, got %v", err)
	}
}

func TestCreateRole_NameConflict(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byName["超级管理员"] = &accessentity.Role{ID: 7, RoleName: "超级管理员", RoleKey: "other"}
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{Role: &accessv1.Role{RoleName: "超级管理员", RoleKey: "superadmin"}})
	if !errors.Is(err, ErrRoleNameConflict) {
		t.Fatalf("expected ErrRoleNameConflict, got %v", err)
	}
}

func TestCreateRole_KeyConflict(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byKey["superadmin"] = &accessentity.Role{ID: 7, RoleName: "other", RoleKey: "superadmin"}
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{Role: &accessv1.Role{RoleName: "超级管理员", RoleKey: "superadmin"}})
	if !errors.Is(err, ErrRoleKeyConflict) {
		t.Fatalf("expected ErrRoleKeyConflict, got %v", err)
	}
}

func TestUpdateRole_ConflictExcludingSelf(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin"}
	repo.byName["管理员"] = repo.byID[5]
	repo.byKey["admin"] = repo.byID[5]
	repo.byName["超级管理员"] = &accessentity.Role{ID: 7, RoleName: "超级管理员", RoleKey: "superadmin"}
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	// Taking another role's name is rejected...
	_, err := app.UpdateRole(context.Background(), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5, RoleName: "超级管理员", RoleKey: "admin"}})
	if !errors.Is(err, ErrRoleNameConflict) {
		t.Fatalf("expected ErrRoleNameConflict, got %v", err)
	}
	// ...but keeping its own values passes.
	_, err = app.UpdateRole(context.Background(), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5, RoleName: "管理员", RoleKey: "admin"}})
	if err != nil {
		t.Fatalf("update keeping own values: %v", err)
	}
}

func TestDeleteRole_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[3] = &accessentity.Role{ID: 3, RoleName: "测试", RoleKey: "test"}
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	if _, err := app.DeleteRole(context.Background(), &accessv1.DeleteRoleRequest{RoleId: 3}); err != nil {
		t.Fatalf("delete role: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 3 {
		t.Fatalf("expected role 3 deleted, got %v", repo.deleted)
	}
}

func TestDeleteRole_NotFound(t *testing.T) {
	app := newRoleApp(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := app.DeleteRole(context.Background(), &accessv1.DeleteRoleRequest{RoleId: 99})
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

// --- Menu Grant (菜单授权) ---

func TestCreateRole_WithMenus_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*accessentity.Menu{
		10: {MenuName: "系统管理"},
		11: {MenuName: "用户管理", ParentID: 10},
	})
	app := newRoleApp(repo, menus, newFakeUserRoleRepo())

	got, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{
		Role:              &accessv1.Role{RoleName: "运营", RoleKey: "operator"},
		MenuIds:           []int64{10, 11},
		MenuCheckStrictly: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create role with menus: %v", err)
	}
	if got.GetMenuCheckStrictly() {
		t.Fatal("expected menu_check_strictly=false to persist")
	}
	gotLinks := repo.links[got.GetRoleId()]
	if len(gotLinks) != 2 || gotLinks[0] != 10 || gotLinks[1] != 11 {
		t.Fatalf("expected grant [10 11], got %v", gotLinks)
	}
}

func TestCreateRole_UnknownMenuRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*accessentity.Menu{10: {MenuName: "系统管理"}})
	app := newRoleApp(repo, menus, newFakeUserRoleRepo())

	_, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{Role: &accessv1.Role{RoleName: "运营", RoleKey: "operator"}, MenuIds: []int64{10, 99}})
	if !errors.Is(err, ErrRoleMenuNotFound) {
		t.Fatalf("expected ErrRoleMenuNotFound, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatal("expected no role to be created when a menu is unknown")
	}
}

func TestCreateRole_MenuDuplicatesDeduped(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*accessentity.Menu{10: {MenuName: "系统管理"}})
	app := newRoleApp(repo, menus, newFakeUserRoleRepo())

	got, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{Role: &accessv1.Role{RoleName: "运营", RoleKey: "operator"}, MenuIds: []int64{10, 10, 10}})
	if err != nil {
		t.Fatalf("create role with duplicate menus: %v", err)
	}
	gotLinks := repo.links[got.GetRoleId()]
	if len(gotLinks) != 1 || gotLinks[0] != 10 {
		t.Fatalf("expected deduped grant [10], got %v", gotLinks)
	}
}

func TestUpdateRoleMenus_ReplacesGrant(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "运营", RoleKey: "operator"}
	menus := newFakeMenuRepoForRole(map[int64]*accessentity.Menu{
		1: {MenuName: "系统管理"},
		2: {MenuName: "用户管理", ParentID: 1},
		3: {MenuName: "角色管理", ParentID: 1},
	})
	app := newRoleApp(repo, menus, newFakeUserRoleRepo())

	reply, err := app.UpdateRoleMenus(context.Background(), &accessv1.UpdateRoleMenusRequest{RoleId: 5, MenuIds: []int64{1, 2, 3}, MenuCheckStrictly: boolPtr(true)})
	if err != nil {
		t.Fatalf("replace grant: %v", err)
	}
	ids := reply.GetMenuIds()
	if len(ids) != 3 || ids[0] != 1 || ids[2] != 3 {
		t.Fatalf("expected echo [1 2 3], got %v", ids)
	}
	if got := repo.links[5]; len(got) != 3 {
		t.Fatalf("expected stored grant of 3 links, got %v", got)
	}
	if !repo.byID[5].MenuCheckStrictly {
		t.Fatal("expected menu_check_strictly=true to persist")
	}

	// Full replacement: an empty list clears the grant and flips the flag.
	reply, err = app.UpdateRoleMenus(context.Background(), &accessv1.UpdateRoleMenusRequest{RoleId: 5, MenuCheckStrictly: boolPtr(false)})
	if err != nil {
		t.Fatalf("clear grant: %v", err)
	}
	if len(reply.GetMenuIds()) != 0 {
		t.Fatalf("expected empty echo, got %v", reply.GetMenuIds())
	}
	if got := repo.links[5]; len(got) != 0 {
		t.Fatalf("expected cleared grant, got %v", got)
	}
	if repo.byID[5].MenuCheckStrictly {
		t.Fatal("expected menu_check_strictly=false to persist")
	}
}

func TestUpdateRoleMenus_UnknownMenuRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "运营", RoleKey: "operator"}
	menus := newFakeMenuRepoForRole(map[int64]*accessentity.Menu{1: {MenuName: "系统管理"}})
	app := newRoleApp(repo, menus, newFakeUserRoleRepo())

	_, err := app.UpdateRoleMenus(context.Background(), &accessv1.UpdateRoleMenusRequest{RoleId: 5, MenuIds: []int64{1, 99}, MenuCheckStrictly: boolPtr(true)})
	if !errors.Is(err, ErrRoleMenuNotFound) {
		t.Fatalf("expected ErrRoleMenuNotFound, got %v", err)
	}
	if len(repo.replaced) != 0 {
		t.Fatal("expected no replacement when a menu is unknown")
	}
}

func TestUpdateRoleMenus_RoleNotFound(t *testing.T) {
	app := newRoleApp(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := app.UpdateRoleMenus(context.Background(), &accessv1.UpdateRoleMenusRequest{RoleId: 99, MenuCheckStrictly: boolPtr(true)})
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestListRoleMenus_StrictDropsHalfCheckedParent(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: true}
	repo.menuParents = map[int64]int64{10: 0, 11: 10, 12: 10, 20: 0}
	repo.links[5] = []int64{10, 11, 12, 20} // 10 is parent of granted 11/12
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	reply, err := app.ListRoleMenus(context.Background(), &accessv1.ListRoleMenusRequest{RoleId: 5})
	if err != nil {
		t.Fatalf("list menus: %v", err)
	}
	ids := reply.GetMenuIds()
	if len(ids) != 3 || ids[0] != 11 || ids[1] != 12 || ids[2] != 20 {
		t.Fatalf("expected [11 12 20] (10 dropped as half-checked parent), got %v", ids)
	}
}

func TestListRoleMenus_StrictDropsFullyCheckedParent(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: true}
	repo.menuParents = map[int64]int64{10: 0, 11: 10}
	repo.links[5] = []int64{10, 11} // parent 10 fully checked via child 11
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	reply, err := app.ListRoleMenus(context.Background(), &accessv1.ListRoleMenusRequest{RoleId: 5})
	if err != nil {
		t.Fatalf("list menus: %v", err)
	}
	ids := reply.GetMenuIds()
	if len(ids) != 1 || ids[0] != 11 {
		t.Fatalf("expected [11] (fully-checked parent 10 excluded), got %v", ids)
	}
}

func TestListRoleMenus_NonStrictReturnsAll(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: false}
	repo.menuParents = map[int64]int64{10: 0, 11: 10, 12: 10, 20: 0}
	repo.links[5] = []int64{10, 11, 12, 20}
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	reply, err := app.ListRoleMenus(context.Background(), &accessv1.ListRoleMenusRequest{RoleId: 5})
	if err != nil {
		t.Fatalf("list menus: %v", err)
	}
	ids := reply.GetMenuIds()
	if len(ids) != 4 || ids[0] != 10 || ids[3] != 20 {
		t.Fatalf("expected verbatim [10 11 12 20], got %v", ids)
	}
}

func TestListRoleMenus_RoleNotFound(t *testing.T) {
	app := newRoleApp(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := app.ListRoleMenus(context.Background(), &accessv1.ListRoleMenusRequest{RoleId: 99})
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

func boolPtr(b bool) *bool { return &b }
