package access

import (
	"context"
	"errors"
	"testing"
	"time"

	accessv1 "glex/api/access/v1"
	accessentity "glex/internal/domain/access/entity"
	accessservice "glex/internal/domain/access/service"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	"glex/pkg/listq"
)

// fakeRoleService is a hand-rolled accessservice.Role fake for the
// application tests: it records every call (including the ctx each write ran
// on and the actor stamped into it) and returns the configured result, so the
// tests exercise orchestration only — transaction wrapping, revocation, DTO
// conversion, pagination, audit stamping — never the domain rules behind the
// interface.
type fakeRoleService struct {
	// Configured returns.
	get          *accessentity.Role
	list         []*accessentity.Role
	create       *accessentity.Role
	update       *accessentity.RoleUpdate
	deleteUsers  []int64
	checked      []int64
	replaceGrant []int64
	replaceUsers []int64
	getErr       error
	listErr      error
	createErr    error
	updateErr    error
	deleteErr    error
	checkedErr   error
	replaceErr   error

	// Recorded calls.
	createCalled   int
	createCtx      context.Context
	createInput    *accessentity.Role
	createMenuIDs  []int64
	listLimit      int
	listOffset     int
	updateCalled   int
	updateCtx      context.Context
	updateInput    *accessentity.Role
	deleteCalled   int
	deleteCtx      context.Context
	deleteID       int64
	deleteActor    int64
	checkedRoleID  int64
	replaceCalled  int
	replaceCtx     context.Context
	replaceRoleID  int64
	replaceMenuIDs []int64
	replaceStrict  bool
	replaceActor   int64
}

func (f *fakeRoleService) Get(_ context.Context, _ int64) (*accessentity.Role, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.get, nil
}

func (f *fakeRoleService) List(_ context.Context, q listq.Query) ([]*accessentity.Role, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.listLimit, f.listOffset = q.Limit, q.Offset
	return f.list, nil
}

func (f *fakeRoleService) Create(ctx context.Context, req accessservice.CreateRoleRequest) (*accessentity.Role, error) {
	f.createCalled++
	f.createCtx, f.createInput, f.createMenuIDs = ctx, req.Role, req.MenuIDs
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.create, nil
}

func (f *fakeRoleService) Update(ctx context.Context, r *accessentity.Role) (*accessentity.RoleUpdate, error) {
	f.updateCalled++
	f.updateCtx, f.updateInput = ctx, r
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.update, nil
}

func (f *fakeRoleService) Delete(ctx context.Context, req accessservice.DeleteRoleRequest) ([]int64, error) {
	f.deleteCalled++
	f.deleteCtx, f.deleteID, f.deleteActor = ctx, req.ID, req.ActorID
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return f.deleteUsers, nil
}

func (f *fakeRoleService) CheckedMenuIDs(_ context.Context, roleID int64) ([]int64, error) {
	f.checkedRoleID = roleID
	if f.checkedErr != nil {
		return nil, f.checkedErr
	}
	return f.checked, nil
}

func (f *fakeRoleService) ReplaceMenus(ctx context.Context, req accessservice.ReplaceRoleMenusRequest) (*accessservice.MenuGrantReplacement, error) {
	f.replaceCalled++
	f.replaceCtx, f.replaceRoleID, f.replaceMenuIDs = ctx, req.RoleID, req.MenuIDs
	f.replaceStrict, f.replaceActor = req.MenuCheckStrictly, req.ActorID
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	return &accessservice.MenuGrantReplacement{MenuIDs: f.replaceGrant, AffectedUserIDs: f.replaceUsers}, nil
}

// errServiceFailed is the propagation sentinel: application tests only assert
// that a service error crosses the handler untouched.
var errServiceFailed = errors.New("service failed")

// errRevokeFailed simulates the Redis revocation failing inside the tx.
var errRevokeFailed = errors.New("revoke failed")

// actorContext returns a ctx authenticating the given System User, so tests
// can assert the audit stamping.
func actorContext(userID int64) context.Context {
	return authnservice.WithPrincipal(context.Background(), authnentity.NewPrincipal(userID, "alice", "session-id", nil, nil))
}

func newRoleApp(svc *fakeRoleService, revoker *fakeSessionRevoker) (*RoleApplication, *fakeTransaction) {
	tx := newFakeTransaction()
	return NewRoleApplication(svc, tx, revoker, testLogger()), tx
}

func boolPtr(b bool) *bool { return &b }

func TestCreateRole_PassesDefaultsActorAndTx(t *testing.T) {
	svc := &fakeRoleService{create: &accessentity.Role{ID: 7, RoleName: "运营", RoleKey: "operator", MenuCheckStrictly: true}}
	revoker := new(fakeSessionRevoker)
	app, tx := newRoleApp(svc, revoker)

	got, err := app.CreateRole(actorContext(42), &accessv1.CreateRoleRequest{
		Role:    &accessv1.Role{RoleName: "运营", RoleKey: "operator"},
		MenuIds: []int64{1, 2},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if svc.createCalled != 1 {
		t.Fatalf("expected exactly one service Create, got %d", svc.createCalled)
	}
	// The write runs on the ctx the transaction body received.
	if svc.createCtx != tx.bodyCtx {
		t.Fatal("service Create must see the ctx the transaction body received")
	}
	// The acting Principal is stamped into the audit fields.
	if svc.createInput.CreateBy != 42 || svc.createInput.UpdateBy != 42 {
		t.Fatalf("audit fields = %d/%d, want the acting principal 42", svc.createInput.CreateBy, svc.createInput.UpdateBy)
	}
	// menu_check_strictly defaults to true (RuoYi default) when absent.
	if !svc.createInput.MenuCheckStrictly {
		t.Fatal("expected the absent menu_check_strictly to default to true before the service call")
	}
	if len(svc.createMenuIDs) != 2 || svc.createMenuIDs[0] != 1 || svc.createMenuIDs[1] != 2 {
		t.Fatalf("menu ids passed to service = %v, want [1 2]", svc.createMenuIDs)
	}
	// The reply is the service result converted to the DTO.
	if got.GetRoleId() != 7 || got.GetRoleKey() != "operator" || !got.GetMenuCheckStrictly() {
		t.Fatalf("reply = %#v", got)
	}
}

func TestCreateRole_StrictFalseAndSystemActorPassThrough(t *testing.T) {
	svc := &fakeRoleService{create: &accessentity.Role{ID: 7, MenuCheckStrictly: false}}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	if _, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{
		Role:              &accessv1.Role{RoleName: "运营", RoleKey: "operator"},
		MenuCheckStrictly: boolPtr(false),
	}); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if svc.createInput.MenuCheckStrictly {
		t.Fatal("expected an explicit menu_check_strictly=false to reach the service")
	}
	// Without a Principal the system itself (0) is the actor.
	if svc.createInput.CreateBy != 0 || svc.createInput.UpdateBy != 0 {
		t.Fatalf("audit fields = %d/%d, want the system actor 0", svc.createInput.CreateBy, svc.createInput.UpdateBy)
	}
}

func TestCreateRole_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleService{createErr: errServiceFailed}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	if _, err := app.CreateRole(context.Background(), &accessv1.CreateRoleRequest{Role: &accessv1.Role{RoleName: "运营", RoleKey: "operator"}}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestGetRole_ConvertsDTO(t *testing.T) {
	now := time.Now()
	svc := &fakeRoleService{get: &accessentity.Role{
		ID: 7, RoleName: "管理员", RoleKey: "admin", RoleSort: 3, Status: 1,
		MenuCheckStrictly: true, Remark: "备注", CreateBy: 1, UpdateBy: 2, CreatedAt: now, UpdatedAt: now,
	}}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	got, err := app.GetRole(context.Background(), &accessv1.GetRoleRequest{RoleId: 7})
	if err != nil {
		t.Fatalf("get role: %v", err)
	}
	if got.GetRoleId() != 7 || got.GetRoleName() != "管理员" || got.GetRoleKey() != "admin" ||
		got.GetRoleSort() != 3 || got.GetStatus() != 1 || !got.GetMenuCheckStrictly() || got.GetRemark() != "备注" ||
		got.GetCreateBy() != 1 || got.GetUpdateBy() != 2 {
		t.Fatalf("reply = %#v", got)
	}
	if !got.GetCreatedAt().AsTime().Equal(now) || !got.GetUpdatedAt().AsTime().Equal(now) {
		t.Fatalf("timestamps = %v/%v", got.GetCreatedAt(), got.GetUpdatedAt())
	}
}

func TestGetRole_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleService{getErr: errServiceFailed}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	if _, err := app.GetRole(context.Background(), &accessv1.GetRoleRequest{RoleId: 7}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestListRoles_IssuesNextPageToken(t *testing.T) {
	svc := &fakeRoleService{list: []*accessentity.Role{{ID: 1}, {ID: 2}}}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	set, err := app.ListRoles(context.Background(), &accessv1.ListRolesRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(set.GetRoles()) != 2 || set.GetRoles()[1].GetRoleId() != 2 {
		t.Fatalf("roles = %#v", set.GetRoles())
	}
	if set.GetNextPageToken() == "" {
		t.Fatal("expected a next page token on a full page")
	}
	if svc.listLimit != 2 || svc.listOffset != 0 {
		t.Fatalf("service saw limit/offset %d/%d, want 2/0", svc.listLimit, svc.listOffset)
	}
	// Following the token resumes at the next offset.
	if _, err := app.ListRoles(context.Background(), &accessv1.ListRolesRequest{PageSize: 2, PageToken: set.GetNextPageToken()}); err != nil {
		t.Fatalf("list roles with token: %v", err)
	}
	if svc.listOffset != 2 {
		t.Fatalf("service saw offset %d after the token, want 2", svc.listOffset)
	}
}

func TestListRoles_NoTokenOnShortPage(t *testing.T) {
	svc := &fakeRoleService{list: []*accessentity.Role{{ID: 1}}}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	set, err := app.ListRoles(context.Background(), &accessv1.ListRolesRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if set.GetNextPageToken() != "" {
		t.Fatalf("expected no next page token on a short page, got %q", set.GetNextPageToken())
	}
}

func TestListRoles_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleService{listErr: errServiceFailed}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	if _, err := app.ListRoles(context.Background(), &accessv1.ListRolesRequest{}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestUpdateRole_RevokesAssignedUsersWhenAuthorizationChanged(t *testing.T) {
	current := &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0}
	disabled := &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 1}
	svc := &fakeRoleService{
		get:    current,
		update: &accessentity.RoleUpdate{Role: disabled, Previous: current, AssignedUserIDs: []int64{7, 8}},
	}
	revoker := new(fakeSessionRevoker)
	app, tx := newRoleApp(svc, revoker)

	got, err := app.UpdateRole(actorContext(42), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5, Status: 1}})
	if err != nil {
		t.Fatalf("update role: %v", err)
	}
	// The reply is the service's persisted role converted to the DTO.
	if got.GetStatus() != 1 {
		t.Fatalf("expected status 1, got %d", got.GetStatus())
	}
	if svc.updateCtx != tx.bodyCtx {
		t.Fatal("service Update must see the ctx the transaction body received")
	}
	if svc.updateInput.UpdateBy != 42 {
		t.Fatalf("update audit actor = %d, want 42", svc.updateInput.UpdateBy)
	}
	if len(revoker.requests) != 1 {
		t.Fatalf("revocation requests = %#v, want exactly one", revoker.requests)
	}
	ids := revoker.requests[0].SystemUserIDs
	if len(ids) != 2 || ids[0] != 7 || ids[1] != 8 {
		t.Fatalf("revoked ids = %v, want the assigned users [7 8]", ids)
	}
}

func TestUpdateRole_NoRevocationWhenAuthorizationUnchanged(t *testing.T) {
	current := &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0, Remark: ""}
	renamed := &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin", Status: 0, Remark: "x"}
	svc := &fakeRoleService{
		get:    current,
		update: &accessentity.RoleUpdate{Role: renamed, Previous: current, AssignedUserIDs: []int64{7}},
	}
	revoker := new(fakeSessionRevoker)
	app, _ := newRoleApp(svc, revoker)

	if _, err := app.UpdateRole(context.Background(), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5, Remark: "x"}}); err != nil {
		t.Fatalf("update role: %v", err)
	}
	if len(revoker.requests) != 0 {
		t.Fatalf("non-authorization change must not revoke, got %#v", revoker.requests)
	}
}

func TestUpdateRole_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleService{
		get:       &accessentity.Role{ID: 5, RoleName: "管理员", RoleKey: "admin"},
		updateErr: errServiceFailed,
	}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	if _, err := app.UpdateRole(context.Background(), &accessv1.UpdateRoleRequest{Role: &accessv1.Role{RoleId: 5}}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestDeleteRole_RevokesAffectedUsers(t *testing.T) {
	svc := &fakeRoleService{deleteUsers: []int64{5}}
	revoker := new(fakeSessionRevoker)
	app, tx := newRoleApp(svc, revoker)

	if _, err := app.DeleteRole(actorContext(42), &accessv1.DeleteRoleRequest{RoleId: 3}); err != nil {
		t.Fatalf("delete role: %v", err)
	}
	if svc.deleteID != 3 || svc.deleteActor != 42 {
		t.Fatalf("service saw delete id/actor %d/%d, want 3/42", svc.deleteID, svc.deleteActor)
	}
	if svc.deleteCtx != tx.bodyCtx {
		t.Fatal("service Delete must see the ctx the transaction body received")
	}
	if len(revoker.requests) != 1 || len(revoker.requests[0].SystemUserIDs) != 1 || revoker.requests[0].SystemUserIDs[0] != 5 {
		t.Fatalf("revocation requests = %#v, want one request revoking user 5", revoker.requests)
	}
}

func TestDeleteRole_EmptyAffectedSkipsRevocation(t *testing.T) {
	svc := &fakeRoleService{}
	revoker := new(fakeSessionRevoker)
	app, _ := newRoleApp(svc, revoker)

	if _, err := app.DeleteRole(context.Background(), &accessv1.DeleteRoleRequest{RoleId: 3}); err != nil {
		t.Fatalf("delete role: %v", err)
	}
	if len(revoker.requests) != 0 {
		t.Fatalf("expected no revocation without affected users, got %#v", revoker.requests)
	}
}

func TestDeleteRole_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleService{deleteErr: errServiceFailed}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	if _, err := app.DeleteRole(context.Background(), &accessv1.DeleteRoleRequest{RoleId: 3}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestListRoleMenus_EchoesCheckedKeys(t *testing.T) {
	svc := &fakeRoleService{checked: []int64{11, 12}}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	reply, err := app.ListRoleMenus(context.Background(), &accessv1.ListRoleMenusRequest{RoleId: 5})
	if err != nil {
		t.Fatalf("list role menus: %v", err)
	}
	if svc.checkedRoleID != 5 {
		t.Fatalf("service saw role %d, want 5", svc.checkedRoleID)
	}
	ids := reply.GetMenuIds()
	if len(ids) != 2 || ids[0] != 11 || ids[1] != 12 {
		t.Fatalf("expected [11 12], got %v", ids)
	}
}

func TestListRoleMenus_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleService{checkedErr: errServiceFailed}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	if _, err := app.ListRoleMenus(context.Background(), &accessv1.ListRoleMenusRequest{RoleId: 5}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestUpdateRoleMenus_EchoesGrantAndRevokes(t *testing.T) {
	svc := &fakeRoleService{replaceGrant: []int64{1, 2}, replaceUsers: []int64{9}}
	revoker := new(fakeSessionRevoker)
	app, tx := newRoleApp(svc, revoker)

	reply, err := app.UpdateRoleMenus(actorContext(42), &accessv1.UpdateRoleMenusRequest{
		RoleId: 5, MenuIds: []int64{1, 2}, MenuCheckStrictly: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("replace grant: %v", err)
	}
	if svc.replaceRoleID != 5 || len(svc.replaceMenuIDs) != 2 || svc.replaceStrict || svc.replaceActor != 42 {
		t.Fatalf("service saw role/menus/strict/actor = %d/%v/%v/%d, want 5/[1 2]/false/42",
			svc.replaceRoleID, svc.replaceMenuIDs, svc.replaceStrict, svc.replaceActor)
	}
	if svc.replaceCtx != tx.bodyCtx {
		t.Fatal("service ReplaceMenus must see the ctx the transaction body received")
	}
	ids := reply.GetMenuIds()
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("expected the persisted grant echoed as [1 2], got %v", ids)
	}
	if len(revoker.requests) != 1 || len(revoker.requests[0].SystemUserIDs) != 1 || revoker.requests[0].SystemUserIDs[0] != 9 {
		t.Fatalf("revocation requests = %#v, want one request revoking user 9", revoker.requests)
	}
}

func TestUpdateRoleMenus_RevocationFailureFailsTheWrite(t *testing.T) {
	svc := &fakeRoleService{replaceGrant: []int64{1}, replaceUsers: []int64{9}}
	revoker := &fakeSessionRevoker{err: errRevokeFailed, failOnRequest: 1}
	app, _ := newRoleApp(svc, revoker)

	// Redis revocation failure rolling back the replacement is part of this
	// operation's business contract.
	if _, err := app.UpdateRoleMenus(context.Background(), &accessv1.UpdateRoleMenusRequest{RoleId: 5, MenuIds: []int64{1}}); !errors.Is(err, errRevokeFailed) {
		t.Fatalf("expected the revocation failure to fail the write, got %v", err)
	}
}

func TestUpdateRoleMenus_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeRoleService{replaceErr: errServiceFailed}
	app, _ := newRoleApp(svc, new(fakeSessionRevoker))

	if _, err := app.UpdateRoleMenus(context.Background(), &accessv1.UpdateRoleMenusRequest{RoleId: 5}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}
