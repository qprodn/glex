package service

import (
	"context"
	"testing"

	"glex/internal/domain/access"
	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"
	"glex/pkg/listq"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeRoleRepo is an in-memory repository.RoleRepo for testing the role
// domain service in isolation from the infrastructure layer. Menu Grant
// state: links maps roleID → granted menu ids (in insertion order);
// menuParents supplies the parent ids for ListRoleMenuLinks.
type fakeRoleRepo struct {
	byID        map[int64]*entity.Role
	byName      map[string]*entity.Role
	byKey       map[string]*entity.Role
	nextID      int64
	created     []*entity.Role
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
		byID:        make(map[int64]*entity.Role),
		byName:      make(map[string]*entity.Role),
		byKey:       make(map[string]*entity.Role),
		links:       make(map[int64][]int64),
		menuParents: make(map[int64]int64),
		nextID:      1,
	}
}

func (r *fakeRoleRepo) FindByID(_ context.Context, id int64) (*entity.Role, error) {
	ro, ok := r.byID[id]
	if !ok {
		return nil, access.ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) FindByRoleName(_ context.Context, roleName string) (*entity.Role, error) {
	ro, ok := r.byName[roleName]
	if !ok {
		return nil, access.ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) FindByRoleKey(_ context.Context, roleKey string) (*entity.Role, error) {
	ro, ok := r.byKey[roleKey]
	if !ok {
		return nil, access.ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) ListRoles(context.Context, listq.Query) ([]*entity.Role, error) {
	return nil, nil
}

func (r *fakeRoleRepo) CreateRole(_ context.Context, ro *entity.Role, menuIDs []int64) (*entity.Role, error) {
	ro.ID = r.nextID
	r.nextID++
	r.byID[ro.ID] = ro
	r.byName[ro.RoleName] = ro
	r.byKey[ro.RoleKey] = ro
	r.created = append(r.created, ro)
	r.links[ro.ID] = append([]int64(nil), menuIDs...)
	return ro, nil
}

func (r *fakeRoleRepo) UpdateRole(_ context.Context, ro *entity.Role) (*entity.RoleUpdate, error) {
	previous := r.byID[ro.ID]
	r.byID[ro.ID] = ro
	return &entity.RoleUpdate{
		Role:            ro,
		Previous:        previous,
		AssignedUserIDs: r.assignedUsers,
	}, nil
}

func (r *fakeRoleRepo) DeleteRole(_ context.Context, id int64, _ int64) ([]int64, error) {
	if _, ok := r.byID[id]; !ok {
		return nil, access.ErrRoleNotFound
	}
	r.deleted = append(r.deleted, id)
	if r.deleteAffected != nil {
		return r.deleteAffected, nil
	}
	return nil, nil
}

func (r *fakeRoleRepo) ListRoleMenuLinks(_ context.Context, roleID int64) ([]*repository.RoleMenuLink, error) {
	ids := r.links[roleID]
	out := make([]*repository.RoleMenuLink, 0, len(ids))
	for _, mid := range ids {
		out = append(out, &repository.RoleMenuLink{MenuID: mid, ParentID: r.menuParents[mid]})
	}
	return out, nil
}

func (r *fakeRoleRepo) ReplaceRoleMenus(_ context.Context, roleID int64, menuIDs []int64, menuCheckStrictly bool, _ int64) ([]int64, error) {
	ro, ok := r.byID[roleID]
	if !ok {
		return nil, access.ErrRoleNotFound
	}
	ro.MenuCheckStrictly = menuCheckStrictly
	r.links[roleID] = append([]int64(nil), menuIDs...)
	r.replaced = append(r.replaced, roleID)
	return append([]int64(nil), r.assignedUsers...), nil
}

// newRoleSvc wires the real role domain service over the fake repos.
func newRoleSvc(roles *fakeRoleRepo, menus *fakeMenuRepo, userRoles *fakeUserRoleRepo) Role {
	return NewRoleService(roles, menus, userRoles, testLogger())
}

func TestCreateRole_PersistsValidatedRole(t *testing.T) {
	repo := newFakeRoleRepo()
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	created, err := svc.Create(context.Background(), &entity.Role{RoleName: "超级管理员", RoleKey: "superadmin", MenuCheckStrictly: true}, nil)
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected a repo-assigned id, got 0")
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created role, got %d", len(repo.created))
	}
	// The DO passes through as-is; the nil→true default for
	// menu_check_strictly lives at the application boundary.
	if !repo.created[0].MenuCheckStrictly {
		t.Fatal("expected menu_check_strictly to persist")
	}
}

func TestCreateRole_BlankNameRejected(t *testing.T) {
	svc := newRoleSvc(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := svc.Create(context.Background(), &entity.Role{RoleName: "", RoleKey: "superadmin"}, nil)
	if !errors.Is(err, access.ErrRoleInvalid) {
		t.Fatalf("expected ErrRoleInvalid, got %v", err)
	}
}

func TestCreateRole_BlankKeyRejected(t *testing.T) {
	svc := newRoleSvc(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := svc.Create(context.Background(), &entity.Role{RoleName: "超级管理员", RoleKey: ""}, nil)
	if !errors.Is(err, access.ErrRoleInvalid) {
		t.Fatalf("expected ErrRoleInvalid, got %v", err)
	}
}

func TestCreateRole_NameConflict(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byName["超级管理员"] = &entity.Role{ID: 7, RoleName: "超级管理员", RoleKey: "other"}
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := svc.Create(context.Background(), &entity.Role{RoleName: "超级管理员", RoleKey: "superadmin"}, nil)
	if !errors.Is(err, access.ErrRoleNameConflict) {
		t.Fatalf("expected ErrRoleNameConflict, got %v", err)
	}
}

func TestCreateRole_KeyConflict(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byKey["superadmin"] = &entity.Role{ID: 7, RoleName: "other", RoleKey: "superadmin"}
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	_, err := svc.Create(context.Background(), &entity.Role{RoleName: "超级管理员", RoleKey: "superadmin"}, nil)
	if !errors.Is(err, access.ErrRoleKeyConflict) {
		t.Fatalf("expected ErrRoleKeyConflict, got %v", err)
	}
}

// --- Menu Grant (菜单授权) ---

func TestCreateRole_WithMenus_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*entity.Menu{
		10: {MenuName: "系统管理"},
		11: {MenuName: "用户管理", ParentID: 10},
	})
	svc := newRoleSvc(repo, menus, newFakeUserRoleRepo())

	created, err := svc.Create(context.Background(), &entity.Role{RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: false}, []int64{10, 11})
	if err != nil {
		t.Fatalf("create role with menus: %v", err)
	}
	if created.MenuCheckStrictly {
		t.Fatal("expected menu_check_strictly=false to persist")
	}
	gotLinks := repo.links[created.ID]
	if len(gotLinks) != 2 || gotLinks[0] != 10 || gotLinks[1] != 11 {
		t.Fatalf("expected grant [10 11], got %v", gotLinks)
	}
}

func TestCreateRole_UnknownMenuRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*entity.Menu{10: {MenuName: "系统管理"}})
	svc := newRoleSvc(repo, menus, newFakeUserRoleRepo())

	_, err := svc.Create(context.Background(), &entity.Role{RoleName: "运营", RoleKey: "operator"}, []int64{10, 99})
	if !errors.Is(err, access.ErrRoleMenuNotFound) {
		t.Fatalf("expected ErrRoleMenuNotFound, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatal("expected no role to be created when a menu is unknown")
	}
}

func TestCreateRole_MenuDuplicatesDeduped(t *testing.T) {
	repo := newFakeRoleRepo()
	menus := newFakeMenuRepoForRole(map[int64]*entity.Menu{10: {MenuName: "系统管理"}})
	svc := newRoleSvc(repo, menus, newFakeUserRoleRepo())

	created, err := svc.Create(context.Background(), &entity.Role{RoleName: "运营", RoleKey: "operator"}, []int64{10, 10, 10})
	if err != nil {
		t.Fatalf("create role with duplicate menus: %v", err)
	}
	gotLinks := repo.links[created.ID]
	if len(gotLinks) != 1 || gotLinks[0] != 10 {
		t.Fatalf("expected deduped grant [10], got %v", gotLinks)
	}
}

func TestUpdateRole_ConflictExcludingSelf(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin"}
	repo.byName["管理员"] = repo.byID[5]
	repo.byKey["admin"] = repo.byID[5]
	repo.byName["超级管理员"] = &entity.Role{ID: 7, RoleName: "超级管理员", RoleKey: "superadmin"}
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	// Taking another role's name is rejected...
	_, err := svc.Update(context.Background(), &entity.Role{ID: 5, RoleName: "超级管理员", RoleKey: "admin"})
	if !errors.Is(err, access.ErrRoleNameConflict) {
		t.Fatalf("expected ErrRoleNameConflict, got %v", err)
	}
	// ...but keeping its own values passes.
	if _, err := svc.Update(context.Background(), &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin"}); err != nil {
		t.Fatalf("update keeping own values: %v", err)
	}
}

// --- delete/disable guards (角色已分配，不能删除/禁用) ---

func TestRoleDelete_AssignedRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[3] = &entity.Role{ID: 3, RoleName: "测试", RoleKey: "test"}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{3}
	svc := newRoleSvc(repo, newFakeMenuRepo(), link)

	_, err := svc.Delete(context.Background(), 3, 0)
	if !errors.Is(err, access.ErrRoleAssigned) {
		t.Fatalf("expected ErrRoleAssigned, got %v", err)
	}
	if len(repo.deleted) != 0 {
		t.Fatal("role must not be deleted when users are assigned")
	}
}

func TestRoleDelete_UnassignedAllowed(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[3] = &entity.Role{ID: 3, RoleName: "测试", RoleKey: "test"}
	repo.deleteAffected = []int64{10}
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	affected, err := svc.Delete(context.Background(), 3, 0)
	if err != nil {
		t.Fatalf("delete unassigned role: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 3 {
		t.Fatalf("expected role 3 deleted, got %v", repo.deleted)
	}
	if len(affected) != 1 || affected[0] != 10 {
		t.Fatalf("expected the repo's assigned identities echoed, got %v", affected)
	}
}

func TestRoleDelete_NotFound(t *testing.T) {
	svc := newRoleSvc(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	if _, err := svc.Delete(context.Background(), 99, 0); !errors.Is(err, access.ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestRoleUpdate_DisableAssignedRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{5}
	svc := newRoleSvc(repo, newFakeMenuRepo(), link)

	_, err := svc.Update(context.Background(), &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1})
	if !errors.Is(err, access.ErrRoleAssigned) {
		t.Fatalf("expected ErrRoleAssigned, got %v", err)
	}
}

func TestRoleUpdate_DisableUnassignedAllowed(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	result, err := svc.Update(context.Background(), &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1})
	if err != nil {
		t.Fatalf("disable unassigned role: %v", err)
	}
	if result.Role.Status != 1 {
		t.Fatalf("expected status 1, got %d", result.Role.Status)
	}
}

func TestRoleUpdate_RedisableAlreadyDisabledPasses(t *testing.T) {
	// Re-saving an already-disabled role must not trip the guard.
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{5}
	svc := newRoleSvc(repo, newFakeMenuRepo(), link)

	if _, err := svc.Update(context.Background(), &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}); err != nil {
		t.Fatalf("re-disabling an already disabled role: %v", err)
	}
}

func TestRoleUpdate_NonStatusChangesPass(t *testing.T) {
	// Updating other fields of an assigned role must pass.
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{5}
	svc := newRoleSvc(repo, newFakeMenuRepo(), link)

	if _, err := svc.Update(context.Background(), &entity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0, Remark: "x"}); err != nil {
		t.Fatalf("non-status update of assigned role: %v", err)
	}
}

// --- Menu Grant replacement and checked keys ---

func TestReplaceMenus_ReplacesGrant(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "运营", RoleKey: "operator"}
	repo.assignedUsers = []int64{10}
	menus := newFakeMenuRepoForRole(map[int64]*entity.Menu{
		1: {MenuName: "系统管理"},
		2: {MenuName: "用户管理", ParentID: 1},
		3: {MenuName: "角色管理", ParentID: 1},
	})
	svc := newRoleSvc(repo, menus, newFakeUserRoleRepo())

	granted, affected, err := svc.ReplaceMenus(context.Background(), 5, []int64{1, 2, 3}, true, 42)
	if err != nil {
		t.Fatalf("replace grant: %v", err)
	}
	if len(granted) != 3 || granted[0] != 1 || granted[2] != 3 {
		t.Fatalf("expected echo [1 2 3], got %v", granted)
	}
	if len(affected) != 1 || affected[0] != 10 {
		t.Fatalf("expected affected [10], got %v", affected)
	}
	if got := repo.links[5]; len(got) != 3 {
		t.Fatalf("expected stored grant of 3 links, got %v", got)
	}
	if !repo.byID[5].MenuCheckStrictly {
		t.Fatal("expected menu_check_strictly=true to persist")
	}

	// Full replacement: an empty list clears the grant and flips the flag.
	granted, _, err = svc.ReplaceMenus(context.Background(), 5, nil, false, 42)
	if err != nil {
		t.Fatalf("clear grant: %v", err)
	}
	if len(granted) != 0 {
		t.Fatalf("expected empty echo, got %v", granted)
	}
	if got := repo.links[5]; len(got) != 0 {
		t.Fatalf("expected cleared grant, got %v", got)
	}
	if repo.byID[5].MenuCheckStrictly {
		t.Fatal("expected menu_check_strictly=false to persist")
	}
}

func TestReplaceMenus_UnknownMenuRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "运营", RoleKey: "operator"}
	menus := newFakeMenuRepoForRole(map[int64]*entity.Menu{1: {MenuName: "系统管理"}})
	svc := newRoleSvc(repo, menus, newFakeUserRoleRepo())

	_, _, err := svc.ReplaceMenus(context.Background(), 5, []int64{1, 99}, true, 42)
	if !errors.Is(err, access.ErrRoleMenuNotFound) {
		t.Fatalf("expected ErrRoleMenuNotFound, got %v", err)
	}
	if len(repo.replaced) != 0 {
		t.Fatal("expected no replacement when a menu is unknown")
	}
}

func TestReplaceMenus_RoleNotFound(t *testing.T) {
	svc := newRoleSvc(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	_, _, err := svc.ReplaceMenus(context.Background(), 99, nil, true, 42)
	if !errors.Is(err, access.ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestCheckedMenuIDs_StrictDropsHalfCheckedParent(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: true}
	repo.menuParents = map[int64]int64{10: 0, 11: 10, 12: 10, 20: 0}
	repo.links[5] = []int64{10, 11, 12, 20} // 10 is parent of granted 11/12
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	ids, err := svc.CheckedMenuIDs(context.Background(), 5)
	if err != nil {
		t.Fatalf("checked menu ids: %v", err)
	}
	if len(ids) != 3 || ids[0] != 11 || ids[1] != 12 || ids[2] != 20 {
		t.Fatalf("expected [11 12 20] (10 dropped as half-checked parent), got %v", ids)
	}
}

func TestCheckedMenuIDs_StrictDropsFullyCheckedParent(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: true}
	repo.menuParents = map[int64]int64{10: 0, 11: 10}
	repo.links[5] = []int64{10, 11} // parent 10 fully checked via child 11
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	ids, err := svc.CheckedMenuIDs(context.Background(), 5)
	if err != nil {
		t.Fatalf("checked menu ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != 11 {
		t.Fatalf("expected [11] (fully-checked parent 10 excluded), got %v", ids)
	}
}

func TestCheckedMenuIDs_NonStrictReturnsAll(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &entity.Role{ID: 5, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: false}
	repo.menuParents = map[int64]int64{10: 0, 11: 10, 12: 10, 20: 0}
	repo.links[5] = []int64{10, 11, 12, 20}
	svc := newRoleSvc(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	ids, err := svc.CheckedMenuIDs(context.Background(), 5)
	if err != nil {
		t.Fatalf("checked menu ids: %v", err)
	}
	if len(ids) != 4 || ids[0] != 10 || ids[3] != 20 {
		t.Fatalf("expected verbatim [10 11 12 20], got %v", ids)
	}
}

func TestCheckedMenuIDs_RoleNotFound(t *testing.T) {
	svc := newRoleSvc(newFakeRoleRepo(), newFakeMenuRepo(), newFakeUserRoleRepo())

	if _, err := svc.CheckedMenuIDs(context.Background(), 99); !errors.Is(err, access.ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}
