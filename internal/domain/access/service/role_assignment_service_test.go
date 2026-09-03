package service

import (
	"context"
	"testing"

	"glex/internal/domain/access"
	"glex/internal/domain/access/entity"
	"glex/internal/domain/access/repository"

	"github.com/go-kratos/kratos/v3/errors"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// fakeUserRoleRepo is an in-memory repository.UserRoleRepo for testing the
// role-assignment and role domain services in isolation from the
// infrastructure layer. roles is the "all active roles" set; links maps
// userID → assigned role ids (in replacement order); users is the
// existing-user set behind UserExists.
type fakeUserRoleRepo struct {
	roles    []*entity.Role
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

func (r *fakeUserRoleRepo) ListRoleAssignments(_ context.Context, userID int64) ([]*entity.RoleAssignment, error) {
	assigned := make(map[int64]struct{}, len(r.links[userID]))
	for _, id := range r.links[userID] {
		assigned[id] = struct{}{}
	}
	out := make([]*entity.RoleAssignment, 0, len(r.roles))
	for _, ro := range r.roles {
		_, ok := assigned[ro.ID]
		out = append(out, &entity.RoleAssignment{Role: ro, Assigned: ok})
	}
	return out, nil
}

func (r *fakeUserRoleRepo) ReplaceUserRoles(_ context.Context, userID int64, roleIDs []int64, _ int64) ([]int64, error) {
	r.links[userID] = append([]int64(nil), roleIDs...)
	r.replaced = append(r.replaced, userID)
	return []int64{userID}, nil
}

func (r *fakeUserRoleRepo) ListRoleUsers(context.Context, int64, ...repository.ListOption) ([]*entity.RoleUser, error) {
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

// newRoleAssignmentSvc wires the real role-assignment domain service over the
// fake repos.
func newRoleAssignmentSvc(roles *fakeRoleRepo, userRoles *fakeUserRoleRepo) RoleAssignment {
	return NewRoleAssignmentService(roles, userRoles, testLogger())
}

// --- Role Assignment (角色分配) — auth dialog ---

func TestListForUser_UserNotFound(t *testing.T) {
	svc := newRoleAssignmentSvc(newFakeRoleRepo(), newFakeUserRoleRepo())

	_, err := svc.ListForUser(context.Background(), 99)
	if !errors.Is(err, access.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestListForUser_AllRolesWithFlags(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*entity.Role{
		{ID: 1, RoleName: "管理员", RoleKey: "admin", RoleSort: 1},
		{ID: 2, RoleName: "运营", RoleKey: "operator", RoleSort: 2},
	}
	link.links[10] = []int64{2}
	svc := newRoleAssignmentSvc(newFakeRoleRepo(), link)

	assignments, err := svc.ListForUser(context.Background(), 10)
	if err != nil {
		t.Fatalf("list user roles: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(assignments))
	}
	if assignments[0].Assigned || !assignments[1].Assigned {
		t.Fatalf("expected assigned flags [false true], got [%v %v]", assignments[0].Assigned, assignments[1].Assigned)
	}
}

func TestReplaceForUser_UserNotFound(t *testing.T) {
	svc := newRoleAssignmentSvc(newFakeRoleRepo(), newFakeUserRoleRepo())

	_, err := svc.ReplaceForUser(context.Background(), 99, []int64{1}, 0)
	if !errors.Is(err, access.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestReplaceForUser_UnknownRoleRejected(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*entity.Role{{ID: 1, RoleName: "管理员", RoleKey: "admin"}}
	svc := newRoleAssignmentSvc(newFakeRoleRepo(), link)

	_, err := svc.ReplaceForUser(context.Background(), 10, []int64{1, 99}, 0)
	if !errors.Is(err, access.ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
	if len(link.replaced) != 0 {
		t.Fatal("replacement must not run when validation fails")
	}
}

func TestReplaceForUser_FullReplacement(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*entity.Role{
		{ID: 1, RoleName: "管理员", RoleKey: "admin", RoleSort: 1},
		{ID: 2, RoleName: "运营", RoleKey: "operator", RoleSort: 2},
		{ID: 3, RoleName: "访客", RoleKey: "guest", RoleSort: 3},
	}
	link.links[10] = []int64{1, 2}
	svc := newRoleAssignmentSvc(newFakeRoleRepo(), link)

	// Replace with [2, 3] — duplicates are silently deduped.
	result, err := svc.ReplaceForUser(context.Background(), 10, []int64{2, 3, 2}, 42)
	if err != nil {
		t.Fatalf("update user roles: %v", err)
	}
	if len(link.replaced) != 1 || link.replaced[0] != 10 {
		t.Fatalf("expected exactly one replacement of user 10, got %v", link.replaced)
	}
	want := []int64{2, 3}
	if len(link.links[10]) != 2 || link.links[10][0] != want[0] || link.links[10][1] != want[1] {
		t.Fatalf("expected links %v, got %v", want, link.links[10])
	}
	// The result carries the updated flags for all roles.
	wantFlags := []bool{false, true, true}
	for i, a := range result.Assignments {
		if a.Assigned != wantFlags[i] {
			t.Fatalf("expected flag %v at %d, got %v", wantFlags[i], i, a.Assigned)
		}
	}
	// The repo echoes the identities whose snapshots the write invalidates.
	if len(result.AffectedUserIDs) != 1 || result.AffectedUserIDs[0] != 10 {
		t.Fatalf("expected affected [10], got %v", result.AffectedUserIDs)
	}
}

func TestReplaceForUser_EmptyClears(t *testing.T) {
	link := userExists(newFakeUserRoleRepo(), 10)
	link.roles = []*entity.Role{{ID: 1, RoleName: "管理员", RoleKey: "admin"}}
	link.links[10] = []int64{1}
	svc := newRoleAssignmentSvc(newFakeRoleRepo(), link)

	result, err := svc.ReplaceForUser(context.Background(), 10, nil, 42)
	if err != nil {
		t.Fatalf("clear user roles: %v", err)
	}
	if len(link.links[10]) != 0 {
		t.Fatalf("expected cleared links, got %v", link.links[10])
	}
	if len(result.Assignments) != 1 || result.Assignments[0].Assigned {
		t.Fatalf("expected single unassigned role in result, got %v", result.Assignments)
	}
}

func TestListRoleUsers_RoleNotFound(t *testing.T) {
	svc := newRoleAssignmentSvc(newFakeRoleRepo(), newFakeUserRoleRepo())

	// The fake repo ignores the filter/order payload; the role gate is what
	// the test exercises.
	_, err := svc.ListRoleUsers(context.Background(), 99, filtering.Filter{}, ordering.OrderBy{}, 20, 0)
	if !errors.Is(err, access.ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}
