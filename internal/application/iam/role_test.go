package iam

import (
	. "glex/internal/domain/iam"

	"context"
	"testing"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeRoleRepo is an in-memory RoleRepo for testing the usecase in isolation
// from the infrastructure layer. Menu Grant state: links maps roleID → granted menu ids
// (in insertion order); menuParents supplies the parent ids for
// ListRoleMenuLinks.
type fakeRoleRepo struct {
	byID        map[int64]*Role
	byName      map[string]*Role
	byKey       map[string]*Role
	nextID      int64
	created     []*Role
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
		byID:        make(map[int64]*Role),
		byName:      make(map[string]*Role),
		byKey:       make(map[string]*Role),
		links:       make(map[int64][]int64),
		menuParents: make(map[int64]int64),
		nextID:      1,
	}
}

func (r *fakeRoleRepo) FindByID(_ context.Context, id int64) (*Role, error) {
	ro, ok := r.byID[id]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) FindByRoleName(_ context.Context, roleName string) (*Role, error) {
	ro, ok := r.byName[roleName]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) FindByRoleKey(_ context.Context, roleKey string) (*Role, error) {
	ro, ok := r.byKey[roleKey]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) ListRoles(context.Context, ...ListOption) ([]*Role, error) {
	return nil, nil
}

func (r *fakeRoleRepo) CreateRole(_ context.Context, ro *Role, menuIDs []int64) (*Role, error) {
	ro.ID = r.nextID
	r.nextID++
	r.byID[ro.ID] = ro
	r.byName[ro.RoleName] = ro
	r.byKey[ro.RoleKey] = ro
	r.created = append(r.created, ro)
	r.links[ro.ID] = append([]int64(nil), menuIDs...)
	return ro, nil
}

func (r *fakeRoleRepo) UpdateRole(_ context.Context, ro *Role) (*RoleUpdate, error) {
	previous := r.byID[ro.ID]
	r.byID[ro.ID] = ro
	return &RoleUpdate{
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

func (r *fakeRoleRepo) ListRoleMenuLinks(_ context.Context, roleID int64) ([]*RoleMenuLink, error) {
	ids := r.links[roleID]
	out := make([]*RoleMenuLink, 0, len(ids))
	for _, mid := range ids {
		out = append(out, &RoleMenuLink{MenuID: mid, ParentID: r.menuParents[mid]})
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
func newFakeMenuRepoForRole(menus map[int64]*Menu) *fakeMenuRepo {
	repo := newFakeMenuRepo()
	for id, m := range menus {
		m.ID = id
		repo.byID[id] = m
	}
	return repo
}

func TestCreateRole_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	got, err := uc.CreateRole(context.Background(), &Role{RoleName: "超级管理员", RoleKey: "superadmin", RoleSort: 1, MenuCheckStrictly: true}, nil)
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected a server-assigned id, got 0")
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created role, got %d", len(repo.created))
	}
	// The DO passes through as-is; the nil→true default lives at the service
	// boundary (request presence), not in iam.
	if !got.MenuCheckStrictly {
		t.Fatal("expected menu_check_strictly to persist")
	}
}

func TestCreateRole_BlankNameRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "", RoleKey: "superadmin"}, nil)
	if !errors.Is(err, ErrRoleInvalid) {
		t.Fatalf("expected ErrRoleInvalid, got %v", err)
	}
}

func TestCreateRole_BlankKeyRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "超级管理员", RoleKey: ""}, nil)
	if !errors.Is(err, ErrRoleInvalid) {
		t.Fatalf("expected ErrRoleInvalid, got %v", err)
	}
}

func TestCreateRole_NameConflict(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byName["超级管理员"] = &Role{ID: 7, RoleName: "超级管理员", RoleKey: "other"}
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "超级管理员", RoleKey: "superadmin"}, nil)
	if !errors.Is(err, ErrRoleNameConflict) {
		t.Fatalf("expected ErrRoleNameConflict, got %v", err)
	}
}

func TestCreateRole_KeyConflict(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byKey["superadmin"] = &Role{ID: 7, RoleName: "other", RoleKey: "superadmin"}
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "超级管理员", RoleKey: "superadmin"}, nil)
	if !errors.Is(err, ErrRoleKeyConflict) {
		t.Fatalf("expected ErrRoleKeyConflict, got %v", err)
	}
}

func TestUpdateRole_ConflictExcludingSelf(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &Role{ID: 5, RoleName: "管理员", RoleKey: "admin"}
	repo.byName["管理员"] = repo.byID[5]
	repo.byKey["admin"] = repo.byID[5]
	repo.byName["超级管理员"] = &Role{ID: 7, RoleName: "超级管理员", RoleKey: "superadmin"}
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	// Taking another role's name is rejected...
	_, err := uc.UpdateRole(context.Background(), &Role{ID: 5, RoleName: "超级管理员", RoleKey: "admin"})
	if !errors.Is(err, ErrRoleNameConflict) {
		t.Fatalf("expected ErrRoleNameConflict, got %v", err)
	}
	// ...but keeping its own values passes.
	_, err = uc.UpdateRole(context.Background(), &Role{ID: 5, RoleName: "管理员", RoleKey: "admin"})
	if err != nil {
		t.Fatalf("update keeping own values: %v", err)
	}
}

func TestDeleteRole_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[3] = &Role{ID: 3, RoleName: "测试", RoleKey: "test"}
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	if err := uc.DeleteRole(context.Background(), 3); err != nil {
		t.Fatalf("delete role: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 3 {
		t.Fatalf("expected role 3 deleted, got %v", repo.deleted)
	}
}

func TestDeleteRole_NotFound(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	err := uc.DeleteRole(context.Background(), 99)
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

// --- Menu Grant (菜单授权) ---

func TestCreateRole_WithMenus_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*Menu{
		10: {MenuName: "系统管理"},
		11: {MenuName: "用户管理", ParentID: 10},
	})
	uc := NewRoleUsecase(repo, menus, newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	got, err := uc.CreateRole(context.Background(), &Role{
		RoleName:          "运营",
		RoleKey:           "operator",
		MenuCheckStrictly: false,
	}, []int64{10, 11})
	if err != nil {
		t.Fatalf("create role with menus: %v", err)
	}
	if got.MenuCheckStrictly {
		t.Fatal("expected menu_check_strictly=false to persist")
	}
	gotLinks := repo.links[got.ID]
	if len(gotLinks) != 2 || gotLinks[0] != 10 || gotLinks[1] != 11 {
		t.Fatalf("expected grant [10 11], got %v", gotLinks)
	}
}

func TestCreateRole_UnknownMenuRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*Menu{10: {MenuName: "系统管理"}})
	uc := NewRoleUsecase(repo, menus, newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "运营", RoleKey: "operator"}, []int64{10, 99})
	if !errors.Is(err, ErrRoleMenuNotFound) {
		t.Fatalf("expected ErrRoleMenuNotFound, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatal("expected no role to be created when a menu is unknown")
	}
}

func TestCreateRole_MenuDuplicatesDeduped(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*Menu{10: {MenuName: "系统管理"}})
	uc := NewRoleUsecase(repo, menus, newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	got, err := uc.CreateRole(context.Background(), &Role{RoleName: "运营", RoleKey: "operator"}, []int64{10, 10, 10})
	if err != nil {
		t.Fatalf("create role with duplicate menus: %v", err)
	}
	gotLinks := repo.links[got.ID]
	if len(gotLinks) != 1 || gotLinks[0] != 10 {
		t.Fatalf("expected deduped grant [10], got %v", gotLinks)
	}
}

func TestUpdateRoleMenus_ReplacesGrant(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &Role{ID: 5, RoleName: "运营", RoleKey: "operator"}
	menus := newFakeMenuRepoForRole(map[int64]*Menu{
		1: {MenuName: "系统管理"},
		2: {MenuName: "用户管理", ParentID: 1},
		3: {MenuName: "角色管理", ParentID: 1},
	})
	uc := NewRoleUsecase(repo, menus, newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	ids, err := uc.UpdateRoleMenus(context.Background(), 5, []int64{1, 2, 3}, true)
	if err != nil {
		t.Fatalf("replace grant: %v", err)
	}
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
	ids, err = uc.UpdateRoleMenus(context.Background(), 5, nil, false)
	if err != nil {
		t.Fatalf("clear grant: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty echo, got %v", ids)
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
	repo.byID[5] = &Role{ID: 5, RoleName: "运营", RoleKey: "operator"}
	menus := newFakeMenuRepoForRole(map[int64]*Menu{1: {MenuName: "系统管理"}})
	uc := NewRoleUsecase(repo, menus, newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.UpdateRoleMenus(context.Background(), 5, []int64{1, 99}, true)
	if !errors.Is(err, ErrRoleMenuNotFound) {
		t.Fatalf("expected ErrRoleMenuNotFound, got %v", err)
	}
	if len(repo.replaced) != 0 {
		t.Fatal("expected no replacement when a menu is unknown")
	}
}

func TestUpdateRoleMenus_RoleNotFound(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.UpdateRoleMenus(context.Background(), 99, nil, true)
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestListRoleMenus_StrictDropsHalfCheckedParent(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: true}
	repo.menuParents = map[int64]int64{10: 0, 11: 10, 12: 10, 20: 0}
	repo.links[5] = []int64{10, 11, 12, 20} // 10 is parent of granted 11/12
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	ids, err := uc.ListRoleMenus(context.Background(), 5)
	if err != nil {
		t.Fatalf("list menus: %v", err)
	}
	if len(ids) != 3 || ids[0] != 11 || ids[1] != 12 || ids[2] != 20 {
		t.Fatalf("expected [11 12 20] (10 dropped as half-checked parent), got %v", ids)
	}
}

func TestListRoleMenus_StrictDropsFullyCheckedParent(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: true}
	repo.menuParents = map[int64]int64{10: 0, 11: 10}
	repo.links[5] = []int64{10, 11} // parent 10 fully checked via child 11
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	ids, err := uc.ListRoleMenus(context.Background(), 5)
	if err != nil {
		t.Fatalf("list menus: %v", err)
	}
	if len(ids) != 1 || ids[0] != 11 {
		t.Fatalf("expected [11] (fully-checked parent 10 excluded), got %v", ids)
	}
}

func TestListRoleMenus_NonStrictReturnsAll(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: false}
	repo.menuParents = map[int64]int64{10: 0, 11: 10, 12: 10, 20: 0}
	repo.links[5] = []int64{10, 11, 12, 20}
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	ids, err := uc.ListRoleMenus(context.Background(), 5)
	if err != nil {
		t.Fatalf("list menus: %v", err)
	}
	if len(ids) != 4 || ids[0] != 10 || ids[3] != 20 {
		t.Fatalf("expected verbatim [10 11 12 20], got %v", ids)
	}
}

func TestListRoleMenus_RoleNotFound(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.ListRoleMenus(context.Background(), 99)
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}
