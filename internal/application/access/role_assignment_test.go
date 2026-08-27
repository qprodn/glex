package access

import (
	. "glex/internal/domain/access"
	accessentity "glex/internal/domain/access/entity"
	accessrepo "glex/internal/domain/access/repository"

	"context"
	"testing"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeUserRoleRepo is an in-memory accessrepo.UserRoleRepo for testing the role-assignment usecase
// and the role delete/disable guards in isolation from the infrastructure layer.
// roles is the "all active roles" set; links maps userID → assigned role ids
// (in replacement order); users is the existing-user set behind UserExists.
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

func (r *fakeUserRoleRepo) ReplaceUserRoles(_ context.Context, userID int64, roleIDs []int64) ([]int64, error) {
	r.links[userID] = append([]int64(nil), roleIDs...)
	r.replaced = append(r.replaced, userID)
	return []int64{userID}, nil
}

// newRoleAssignmentUsecase wires the role-assignment usecase with the test
// transaction; flows under test reach the session world only through the
// injected revocation fake.
func newRoleAssignmentUsecase(link accessrepo.UserRoleRepo) *RoleAssignmentUsecase {
	return NewRoleAssignmentUsecase(newFakeRoleRepo(), link, new(fakeSessionRevoker), newFakeTransaction(), testLogger())
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

// userExists seeds the link fake's existing-user set.
func userExists(link *fakeUserRoleRepo, userIDs ...int64) *fakeUserRoleRepo {
	for _, id := range userIDs {
		link.users[id] = struct{}{}
	}
	return link
}

// --- accessentity.Role Assignment (角色分配) — auth usecase ---

func TestAuthListUserRoles_UserNotFound(t *testing.T) {
	uc := newRoleAssignmentUsecase(newFakeUserRoleRepo())

	_, err := uc.ListUserRoles(context.Background(), 99)
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
	uc := newRoleAssignmentUsecase(link)

	got, err := uc.ListUserRoles(context.Background(), 10)
	if err != nil {
		t.Fatalf("list user roles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(got))
	}
	if got[0].Assigned || !got[1].Assigned {
		t.Fatalf("expected assigned flags [false true], got [%v %v]", got[0].Assigned, got[1].Assigned)
	}
}

func TestAuthUpdateUserRoles_UserNotFound(t *testing.T) {
	uc := newRoleAssignmentUsecase(newFakeUserRoleRepo())

	_, err := uc.UpdateUserRoles(context.Background(), 99, []int64{1})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestAuthUpdateUserRoles_UnknownRoleRejected(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*accessentity.Role{{ID: 1, RoleName: "管理员", RoleKey: "admin"}}
	uc := newRoleAssignmentUsecase(link)

	_, err := uc.UpdateUserRoles(context.Background(), 10, []int64{1, 99})
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
	uc := NewRoleAssignmentUsecase(newFakeRoleRepo(), link, revoker, newFakeTransaction(), testLogger())

	// Replace with [2, 3] — duplicates are silently deduped.
	got, err := uc.UpdateUserRoles(context.Background(), 10, []int64{2, 3, 2})
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
	for i, a := range got {
		if a.Assigned != wantFlags[i] {
			t.Fatalf("expected flag %v at %d, got %v", wantFlags[i], i, a.Assigned)
		}
	}
}

func TestAuthUpdateUserRoles_EmptyClears(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*accessentity.Role{{ID: 1, RoleName: "管理员", RoleKey: "admin"}}
	link.links[10] = []int64{1}
	uc := newRoleAssignmentUsecase(link)

	got, err := uc.UpdateUserRoles(context.Background(), 10, nil)
	if err != nil {
		t.Fatalf("clear user roles: %v", err)
	}
	if len(link.links[10]) != 0 {
		t.Fatalf("expected cleared links, got %v", link.links[10])
	}
	if len(got) != 1 || got[0].Assigned {
		t.Fatalf("expected single unassigned role in response, got %v", got)
	}
}

func TestAuthListRoleUsers_RoleNotFound(t *testing.T) {
	uc := newRoleAssignmentUsecase(newFakeUserRoleRepo())

	_, err := uc.ListRoleUsers(context.Background(), 99)
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
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), link, newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	err := uc.DeleteRole(context.Background(), 3)
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
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	if err := uc.DeleteRole(context.Background(), 3); err != nil {
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
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), link, newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	_, err := uc.UpdateRole(context.Background(), &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1})
	if !errors.Is(err, ErrRoleAssigned) {
		t.Fatalf("expected ErrRoleAssigned, got %v", err)
	}
}

func TestRoleUpdate_DisableUnassignedAllowed(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), newFakeUserRoleRepo(), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	got, err := uc.UpdateRole(context.Background(), &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1})
	if err != nil {
		t.Fatalf("disable unassigned role: %v", err)
	}
	if got.Status != 1 {
		t.Fatalf("expected status 1, got %d", got.Status)
	}
}

func TestRoleUpdate_RedisableAlreadyDisabledPasses(t *testing.T) {
	// Re-saving an already-disabled role must not trip the guard.
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{5}
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), link, newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	if _, err := uc.UpdateRole(context.Background(), &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}); err != nil {
		t.Fatalf("re-disabling an already disabled role: %v", err)
	}
}

func TestRoleUpdate_NonStatusChangesPass(t *testing.T) {
	// Updating other fields of an assigned role must pass.
	repo := newFakeRoleRepo()
	repo.byID[5] = &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	link := newFakeUserRoleRepo()
	link.links[10] = []int64{5}
	uc := NewRoleUsecase(repo, newFakeMenuRepo(), link, newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	if _, err := uc.UpdateRole(context.Background(), &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0, Remark: "x"}); err != nil {
		t.Fatalf("non-status update of assigned role: %v", err)
	}
}
