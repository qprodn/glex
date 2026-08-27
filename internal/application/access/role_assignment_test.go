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

// fakeUserRoleRepo is an in-memory accessrepo.UserRoleRepo for testing the
// role-assignment application service and the role delete/disable guards in
// isolation from the infrastructure layer. roles is the "all active roles"
// set; links maps userID → assigned role ids (in replacement order); users is
// the existing-user set behind UserExists.
type fakeUserRoleRepo struct {
	roles    []*accessentity.Role
	links    map[int64][]int64
	users    map[int64]struct{}
	replaced []int64
}

func newFakeUserRoleRepo() *fakeUserRoleRepo {
	return &fakeUserRoleRepo{links: make(map[int64][]int64), users: make(map[int64]struct{})}
}

func (r *fakeUserRoleRepo) UserExists(_ context.Context, userID int64) (bool, error) {
	_, ok := r.users[userID]
	return ok, nil
}

func (r *fakeUserRoleRepo) ListRoleAssignments(_ context.Context, userID int64) ([]*accessrepo.RoleAssignment, error) {
	assigned := make(map[int64]struct{}, len(r.links[userID]))
	for _, id := range r.links[userID] {
		assigned[id] = struct{}{}
	}
	out := make([]*accessrepo.RoleAssignment, 0, len(r.roles))
	for _, ro := range r.roles {
		_, ok := assigned[ro.ID]
		out = append(out, &accessrepo.RoleAssignment{Role: ro, Assigned: ok})
	}
	return out, nil
}

func (r *fakeUserRoleRepo) ReplaceUserRoles(_ context.Context, userID int64, roleIDs []int64, _ int64) ([]int64, error) {
	r.links[userID] = append([]int64(nil), roleIDs...)
	r.replaced = append(r.replaced, userID)
	return []int64{userID}, nil
}

func (r *fakeUserRoleRepo) ListRoleUsers(context.Context, int64, ...accessrepo.ListOption) ([]*accessrepo.RoleUser, error) {
	return nil, nil
}

func (r *fakeUserRoleRepo) CountUserRoles(_ context.Context, roleID int64) (int, error) {
	n := 0
	for _, ids := range r.links {
		for _, id := range ids {
			if id == roleID {
				n++
			}
		}
	}
	return n, nil
}

// newRoleAssignmentApp wires the role-assignment application service with the
// test transaction; flows under test reach the session world only through the
// injected revocation fake.
func newRoleAssignmentApp(link *fakeUserRoleRepo) *RoleAssignmentApplication {
	return NewRoleAssignmentApplication(newFakeRoleRepo(), link, new(fakeSessionRevoker), newFakeTransaction(), testLogger())
}

// userExists seeds the link fake's existing-user set.
func userExists(link *fakeUserRoleRepo, userIDs ...int64) *fakeUserRoleRepo {
	for _, id := range userIDs {
		link.users[id] = struct{}{}
	}
	return link
}

// --- Role Assignment (角色分配) — auth dialog ---

func TestAuthListUserRoles_UserNotFound(t *testing.T) {
	app := newRoleAssignmentApp(newFakeUserRoleRepo())

	_, err := app.ListUserRoles(context.Background(), &accessv1.ListUserRolesRequest{UserId: 99})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestAuthListUserRoles_AllRolesWithFlags(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*accessentity.Role{
		{ID: 1, RoleName: "管理员", RoleKey: "admin", RoleSort: 1},
		{ID: 2, RoleName: "运营", RoleKey: "operator", RoleSort: 2},
	}
	link.links[10] = []int64{2}
	app := newRoleAssignmentApp(link)

	got, err := app.ListUserRoles(context.Background(), &accessv1.ListUserRolesRequest{UserId: 10})
	if err != nil {
		t.Fatalf("list user roles: %v", err)
	}
	items := got.GetItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(items))
	}
	if items[0].GetAssigned() || !items[1].GetAssigned() {
		t.Fatalf("expected assigned flags [false true], got [%v %v]", items[0].GetAssigned(), items[1].GetAssigned())
	}
}

func TestAuthUpdateUserRoles_UserNotFound(t *testing.T) {
	app := newRoleAssignmentApp(newFakeUserRoleRepo())

	_, err := app.UpdateUserRoles(context.Background(), &accessv1.UpdateUserRolesRequest{UserId: 99, RoleIds: []int64{1}})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestAuthUpdateUserRoles_UnknownRoleRejected(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*accessentity.Role{{ID: 1, RoleName: "管理员", RoleKey: "admin"}}
	app := newRoleAssignmentApp(link)

	_, err := app.UpdateUserRoles(context.Background(), &accessv1.UpdateUserRolesRequest{UserId: 10, RoleIds: []int64{1, 99}})
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
	if len(link.replaced) != 0 {
		t.Fatal("replacement must not run when validation fails")
	}
}

func TestAuthUpdateUserRoles_FullReplacement(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*accessentity.Role{
		{ID: 1, RoleName: "管理员", RoleKey: "admin", RoleSort: 1},
		{ID: 2, RoleName: "运营", RoleKey: "operator", RoleSort: 2},
		{ID: 3, RoleName: "访客", RoleKey: "guest", RoleSort: 3},
	}
	link.links[10] = []int64{1, 2}
	revoker := &fakeSessionRevoker{}
	app := NewRoleAssignmentApplication(newFakeRoleRepo(), link, revoker, newFakeTransaction(), testLogger())

	// Replace with [2, 3] — duplicates are silently deduped.
	got, err := app.UpdateUserRoles(context.Background(), &accessv1.UpdateUserRolesRequest{UserId: 10, RoleIds: []int64{2, 3, 2}})
	if err != nil {
		t.Fatalf("update user roles: %v", err)
	}
	if len(link.replaced) != 1 || link.replaced[0] != 10 {
		t.Fatalf("expected exactly one replacement of user 10, got %v", link.replaced)
	}
	if len(revoker.requests) != 1 || len(revoker.requests[0].SystemUserIDs) != 1 || revoker.requests[0].SystemUserIDs[0] != 10 {
		t.Fatalf("revocation requests = %#v, want one request revoking user 10", revoker.requests)
	}
	want := []int64{2, 3}
	if len(link.links[10]) != 2 || link.links[10][0] != want[0] || link.links[10][1] != want[1] {
		t.Fatalf("expected links %v, got %v", want, link.links[10])
	}
	// The response carries the updated flags for all roles.
	wantFlags := []bool{false, true, true}
	for i, a := range got.GetItems() {
		if a.GetAssigned() != wantFlags[i] {
			t.Fatalf("expected flag %v at %d, got %v", wantFlags[i], i, a.GetAssigned())
		}
	}
}

func TestAuthUpdateUserRoles_EmptyClears(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*accessentity.Role{{ID: 1, RoleName: "管理员", RoleKey: "admin"}}
	link.links[10] = []int64{1}
	app := newRoleAssignmentApp(link)

	got, err := app.UpdateUserRoles(context.Background(), &accessv1.UpdateUserRolesRequest{UserId: 10})
	if err != nil {
		t.Fatalf("clear user roles: %v", err)
	}
	if len(link.links[10]) != 0 {
		t.Fatalf("expected cleared links, got %v", link.links[10])
	}
	items := got.GetItems()
	if len(items) != 1 || items[0].GetAssigned() {
		t.Fatalf("expected single unassigned role in response, got %v", items)
	}
}

func TestAuthListRoleUsers_RoleNotFound(t *testing.T) {
	app := newRoleAssignmentApp(newFakeUserRoleRepo())

	_, err := app.ListRoleUsers(context.Background(), &accessv1.ListRoleUsersRequest{RoleId: 99})
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

// --- delete/disable guards (角色已分配，不能删除/禁用) ---

func TestRoleDelete_AssignedRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[3] = &accessentity.Role{ID: 3, RoleName: "测试", RoleKey: "test"}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{3}
	app := newRoleApp(repo, newFakeMenuRepo(), link)

	_, err := app.DeleteRole(context.Background(), &accessv1.DeleteRoleRequest{RoleId: 3})
	if !errors.Is(err, ErrRoleAssigned) {
		t.Fatalf("expected ErrRoleAssigned, got %v", err)
	}
	if len(repo.deleted) != 0 {
		t.Fatal("role must not be deleted when users are assigned")
	}
}

func TestRoleDelete_UnassignedAllowed(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[3] = &accessentity.Role{ID: 3, RoleName: "测试", RoleKey: "test"}
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	if _, err := app.DeleteRole(context.Background(), &accessv1.DeleteRoleRequest{RoleId: 3}); err != nil {
		t.Fatalf("delete unassigned role: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 3 {
		t.Fatalf("expected role 3 deleted, got %v", repo.deleted)
	}
}

func TestRoleUpdate_DisableAssignedRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{5}
	app := newRoleApp(repo, newFakeMenuRepo(), link)

	_, err := app.UpdateRole(context.Background(), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}})
	if !errors.Is(err, ErrRoleAssigned) {
		t.Fatalf("expected ErrRoleAssigned, got %v", err)
	}
}

func TestRoleUpdate_DisableUnassignedAllowed(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	app := newRoleApp(repo, newFakeMenuRepo(), newFakeUserRoleRepo())

	got, err := app.UpdateRole(context.Background(), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}})
	if err != nil {
		t.Fatalf("disable unassigned role: %v", err)
	}
	if got.GetStatus() != 1 {
		t.Fatalf("expected status 1, got %d", got.GetStatus())
	}
}

func TestRoleUpdate_RedisableAlreadyDisabledPasses(t *testing.T) {
	// Re-saving an already-disabled role must not trip the guard.
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{5}
	app := newRoleApp(repo, newFakeMenuRepo(), link)

	if _, err := app.UpdateRole(context.Background(), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}}); err != nil {
		t.Fatalf("re-disabling an already disabled role: %v", err)
	}
}

func TestRoleUpdate_NonStatusChangesPass(t *testing.T) {
	// Updating other fields of an assigned role must pass.
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{5}
	app := newRoleApp(repo, newFakeMenuRepo(), link)

	if _, err := app.UpdateRole(context.Background(), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5, RoleName: "管理员", RoleKey: "admin", Status: 0, Remark: "x"}}); err != nil {
		t.Fatalf("non-status update of assigned role: %v", err)
	}
}
