package access

import (
	"context"
	"errors"
	"testing"
	"time"

	accessv1 "glex/api/access/v1"
	accessentity "glex/internal/domain/access/entity"
	accessservice "glex/internal/domain/access/service"
	"glex/pkg/listq"
)

// fakeRoleAssignmentService is a hand-rolled accessservice.RoleAssignment
// fake for the application tests: it records every call and returns the
// configured result, so the tests exercise orchestration only — transaction
// wrapping, revocation, DTO conversion, pagination, audit stamping — never
// the domain rules behind the interface.
type fakeRoleAssignmentService struct {
	// Configured returns.
	listed      []*accessentity.RoleAssignment
	replacement *accessservice.RoleAssignmentReplacement
	roleUsers   []*accessentity.RoleUser
	listErr     error
	replaceErr  error
	usersErr    error

	// Recorded calls.
	listUserID     int64
	replaceCalled  int
	replaceCtx     context.Context
	replaceUserID  int64
	replaceRoleIDs []int64
	replaceActor   int64
	usersRoleID    int64
	usersLimit     int
	usersOffset    int
}

func (f *fakeRoleAssignmentService) ListForUser(_ context.Context, userID int64) ([]*accessentity.RoleAssignment, error) {
	f.listUserID = userID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listed, nil
}

func (f *fakeRoleAssignmentService) ReplaceForUser(ctx context.Context, userID int64, roleIDs []int64, actorID int64) (*accessservice.RoleAssignmentReplacement, error) {
	f.replaceCalled++
	f.replaceCtx, f.replaceUserID, f.replaceRoleIDs, f.replaceActor = ctx, userID, roleIDs, actorID
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	return f.replacement, nil
}

func (f *fakeRoleAssignmentService) ListRoleUsers(_ context.Context, roleID int64, q listq.Query) ([]*accessentity.RoleUser, error) {
	f.usersRoleID, f.usersLimit, f.usersOffset = roleID, q.Limit, q.Offset
	if f.usersErr != nil {
		return nil, f.usersErr
	}
	return f.roleUsers, nil
}

// newRoleAssignmentApp wires the role-assignment application service with the
// test transaction; flows under test reach the session world only through the
// injected revocation fake.
func newRoleAssignmentApp(svc *fakeRoleAssignmentService, revoker *fakeSessionRevoker) (*RoleAssignmentApplication, *fakeTransaction) {
	tx := newFakeTransaction()
	return NewRoleAssignmentApplication(svc, revoker, tx, testLogger()), tx
}

// --- Role Assignment (角色分配) — auth dialog ---

func TestListUserRoles_ConvertsAssignments(t *testing.T) {
	svc := &fakeRoleAssignmentService{listed: []*accessentity.RoleAssignment{
		{Role: &accessentity.Role{ID: 1, RoleName: "管理员", RoleKey: "admin"}, Assigned: false},
		{Role: &accessentity.Role{ID: 2, RoleName: "运营", RoleKey: "operator"}, Assigned: true},
	}}
	app, _ := newRoleAssignmentApp(svc, new(fakeSessionRevoker))

	got, err := app.ListUserRoles(context.Background(), &accessv1.ListUserRolesRequest{UserId: 10})
	if err != nil {
		t.Fatalf("list user roles: %v", err)
	}
	if svc.listUserID != 10 {
		t.Fatalf("service saw user %d, want 10", svc.listUserID)
	}
	items := got.GetItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(items))
	}
	if items[0].GetAssigned() || !items[1].GetAssigned() {
		t.Fatalf("expected assigned flags [false true], got [%v %v]", items[0].GetAssigned(), items[1].GetAssigned())
	}
	if items[1].GetRole().GetRoleKey() != "operator" {
		t.Fatalf("role DTO = %#v", items[1].GetRole())
	}
}

func TestListUserRoles_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleAssignmentService{listErr: errServiceFailed}
	app, _ := newRoleAssignmentApp(svc, new(fakeSessionRevoker))

	if _, err := app.ListUserRoles(context.Background(), &accessv1.ListUserRolesRequest{UserId: 10}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestUpdateUserRoles_RevokesAffectedAndReturnsFlags(t *testing.T) {
	svc := &fakeRoleAssignmentService{replacement: &accessservice.RoleAssignmentReplacement{
		Assignments: []*accessentity.RoleAssignment{
			{Role: &accessentity.Role{ID: 1, RoleName: "管理员", RoleKey: "admin"}, Assigned: false},
			{Role: &accessentity.Role{ID: 2, RoleName: "运营", RoleKey: "operator"}, Assigned: true},
		},
		AffectedUserIDs: []int64{10},
	}}
	revoker := new(fakeSessionRevoker)
	app, tx := newRoleAssignmentApp(svc, revoker)

	got, err := app.UpdateUserRoles(actorContext(42), &accessv1.UpdateUserRolesRequest{UserId: 10, RoleIds: []int64{2}})
	if err != nil {
		t.Fatalf("update user roles: %v", err)
	}
	if svc.replaceCalled != 1 || svc.replaceUserID != 10 || len(svc.replaceRoleIDs) != 1 || svc.replaceRoleIDs[0] != 2 {
		t.Fatalf("service saw user/roles = %d/%v, want 10/[2]", svc.replaceUserID, svc.replaceRoleIDs)
	}
	if svc.replaceActor != 42 {
		t.Fatalf("replace audit actor = %d, want 42", svc.replaceActor)
	}
	if svc.replaceCtx != tx.bodyCtx {
		t.Fatal("service ReplaceForUser must see the ctx the transaction body received")
	}
	// The replacement revokes the user whose snapshot the write invalidates.
	if len(revoker.requests) != 1 || len(revoker.requests[0].SystemUserIDs) != 1 || revoker.requests[0].SystemUserIDs[0] != 10 {
		t.Fatalf("revocation requests = %#v, want one request revoking user 10", revoker.requests)
	}
	// The response carries the service's assignments as DTOs.
	wantFlags := []bool{false, true}
	for i, a := range got.GetItems() {
		if a.GetAssigned() != wantFlags[i] {
			t.Fatalf("expected flag %v at %d, got %v", wantFlags[i], i, a.GetAssigned())
		}
	}
}

func TestUpdateUserRoles_RevocationFailureFailsTheWrite(t *testing.T) {
	svc := &fakeRoleAssignmentService{replacement: &accessservice.RoleAssignmentReplacement{
		AffectedUserIDs: []int64{10},
	}}
	revoker := &fakeSessionRevoker{err: errRevokeFailed, failOnRequest: 1}
	app, _ := newRoleAssignmentApp(svc, revoker)

	// Redis revocation failure rolling back the PostgreSQL replacement is
	// part of this operation's business contract.
	if _, err := app.UpdateUserRoles(context.Background(), &accessv1.UpdateUserRolesRequest{UserId: 10, RoleIds: []int64{1}}); !errors.Is(err, errRevokeFailed) {
		t.Fatalf("expected the revocation failure to fail the write, got %v", err)
	}
}

func TestUpdateUserRoles_EmptyAffectedSkipsRevocation(t *testing.T) {
	svc := &fakeRoleAssignmentService{replacement: &accessservice.RoleAssignmentReplacement{}}
	revoker := new(fakeSessionRevoker)
	app, _ := newRoleAssignmentApp(svc, revoker)

	if _, err := app.UpdateUserRoles(context.Background(), &accessv1.UpdateUserRolesRequest{UserId: 10}); err != nil {
		t.Fatalf("update user roles: %v", err)
	}
	if len(revoker.requests) != 0 {
		t.Fatalf("expected no revocation without affected users, got %#v", revoker.requests)
	}
}

func TestUpdateUserRoles_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleAssignmentService{replaceErr: errServiceFailed}
	app, _ := newRoleAssignmentApp(svc, new(fakeSessionRevoker))

	if _, err := app.UpdateUserRoles(context.Background(), &accessv1.UpdateUserRolesRequest{UserId: 10, RoleIds: []int64{1}}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestListRoleUsers_PaginatesAndConverts(t *testing.T) {
	loginAt := time.Now()
	svc := &fakeRoleAssignmentService{roleUsers: []*accessentity.RoleUser{
		{ID: 1, UserName: "alice", Status: 0, LoginDate: &loginAt},
		{ID: 2, UserName: "bob", Status: 1},
	}}
	app, _ := newRoleAssignmentApp(svc, new(fakeSessionRevoker))

	set, err := app.ListRoleUsers(context.Background(), &accessv1.ListRoleUsersRequest{RoleId: 5, PageSize: 2})
	if err != nil {
		t.Fatalf("list role users: %v", err)
	}
	if svc.usersRoleID != 5 || svc.usersLimit != 2 || svc.usersOffset != 0 {
		t.Fatalf("service saw role/limit/offset = %d/%d/%d, want 5/2/0", svc.usersRoleID, svc.usersLimit, svc.usersOffset)
	}
	users := set.GetUsers()
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	if users[0].GetUserName() != "alice" || !users[0].GetLoginDate().AsTime().Equal(loginAt) {
		t.Fatalf("user DTO = %#v", users[0])
	}
	if users[1].GetUserName() != "bob" || users[1].GetLoginDate() != nil {
		t.Fatalf("user DTO = %#v", users[1])
	}
	if set.GetNextPageToken() == "" {
		t.Fatal("expected a next page token on a full page")
	}

	// Following the token resumes at the next offset; a short page yields no
	// further token.
	svc.roleUsers = []*accessentity.RoleUser{{ID: 3, UserName: "carol"}}
	set, err = app.ListRoleUsers(context.Background(), &accessv1.ListRoleUsersRequest{RoleId: 5, PageSize: 2, PageToken: set.GetNextPageToken()})
	if err != nil {
		t.Fatalf("list role users with token: %v", err)
	}
	if svc.usersOffset != 2 {
		t.Fatalf("service saw offset %d after the token, want 2", svc.usersOffset)
	}
	if set.GetNextPageToken() != "" {
		t.Fatalf("expected no next page token on a short page, got %q", set.GetNextPageToken())
	}
}

func TestListRoleUsers_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleAssignmentService{usersErr: errServiceFailed}
	app, _ := newRoleAssignmentApp(svc, new(fakeSessionRevoker))

	if _, err := app.ListRoleUsers(context.Background(), &accessv1.ListRoleUsersRequest{RoleId: 5}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}
