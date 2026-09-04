package org

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/fieldmaskpb"

	orgv1 "glex/api/org/v1"

	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	orgdomain "glex/internal/domain/org"
	orgentity "glex/internal/domain/org/entity"
)

// fakeDeptService is a hand-rolled orgservice.Dept fake for the application
// tests: it records every call (including the ctx each write ran on) and
// returns the configured result, so the tests exercise orchestration only —
// transaction wrapping, DTO conversion, tree assembly, audit stamping —
// never the domain rules behind the interface.
type fakeDeptService struct {
	// Configured returns.
	get       *orgentity.Dept
	list      []*orgentity.Dept
	create    *orgentity.Dept
	update    *orgentity.Dept
	getErr    error
	listErr   error
	createErr error
	updateErr error
	deleteErr error

	// Recorded calls.
	createCalled int
	createCtx    context.Context
	createInput  *orgentity.Dept
	updateCalled int
	updateCtx    context.Context
	updateInput  *orgentity.Dept
	deleteCalled int
	deleteCtx    context.Context
	deleteID     int64
	deleteActor  int64
}

func (f *fakeDeptService) Get(_ context.Context, _ int64) (*orgentity.Dept, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.get, nil
}

func (f *fakeDeptService) List(_ context.Context) ([]*orgentity.Dept, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeDeptService) Create(ctx context.Context, d *orgentity.Dept) (*orgentity.Dept, error) {
	f.createCalled++
	f.createCtx, f.createInput = ctx, d
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.create, nil
}

func (f *fakeDeptService) Update(ctx context.Context, d *orgentity.Dept) (*orgentity.Dept, error) {
	f.updateCalled++
	f.updateCtx, f.updateInput = ctx, d
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.update, nil
}

func (f *fakeDeptService) Delete(ctx context.Context, id, actorID int64) error {
	f.deleteCalled++
	f.deleteCtx, f.deleteID, f.deleteActor = ctx, id, actorID
	return f.deleteErr
}

func (f *fakeDeptService) SubtreeIDs(context.Context, int64) ([]int64, error) {
	return nil, nil
}

func (f *fakeDeptService) EnsureActive(context.Context, int64) error {
	return nil
}

// errServiceFailed is the propagation sentinel: application tests only assert
// that a service error crosses the handler untouched.
var errServiceFailed = errors.New("service failed")

// actorContext returns a ctx authenticating the given System User, so tests
// can assert the audit stamping.
func actorContext(userID int64) context.Context {
	return authnservice.WithPrincipal(context.Background(), authnentity.NewPrincipal(userID, "alice", "session-id", nil, nil))
}

// fakeDeptUsage records the crossdomain user-contract asks: whether a
// department is still referenced by users.
type fakeDeptUsage struct {
	calls []int64
	has   bool
	err   error
}

func (f *fakeDeptUsage) HasUsersInDept(_ context.Context, deptID int64) (bool, error) {
	f.calls = append(f.calls, deptID)
	return f.has, f.err
}

func newDeptApp(svc *fakeDeptService, usage *fakeDeptUsage) (*DeptApplication, *fakeTransaction) {
	tx := newFakeTransaction()
	return NewDeptApplication(svc, tx, usage, testLogger()), tx
}

func TestCreateDept_StampsActorAndRunsInTx(t *testing.T) {
	svc := &fakeDeptService{create: &orgentity.Dept{ID: 11, DeptName: "研发部"}}
	app, tx := newDeptApp(svc, new(fakeDeptUsage))

	got, err := app.CreateDept(actorContext(42), &orgv1.CreateDeptRequest{
		Dept: &orgv1.Dept{ParentId: 1, DeptName: "研发部", OrderNum: 3},
	})
	if err != nil {
		t.Fatalf("CreateDept: %v", err)
	}
	if got.DeptId != 11 {
		t.Fatalf("DeptId = %d, want 11", got.DeptId)
	}
	if svc.createCalled != 1 || tx.began != 1 {
		t.Fatalf("create called %d, tx began %d, want 1/1", svc.createCalled, tx.began)
	}
	if !inTx(svc.createCtx) {
		t.Fatal("Create ran outside the transaction ctx")
	}
	if svc.createInput.CreateBy != 42 || svc.createInput.UpdateBy != 42 {
		t.Fatalf("audit actor = %d/%d, want 42/42", svc.createInput.CreateBy, svc.createInput.UpdateBy)
	}
	if svc.createInput.OrderNum != 3 {
		t.Fatalf("OrderNum = %d, want 3", svc.createInput.OrderNum)
	}
}

func TestCreateDept_PropagatesServiceError(t *testing.T) {
	svc := &fakeDeptService{createErr: errServiceFailed}
	app, _ := newDeptApp(svc, new(fakeDeptUsage))
	if _, err := app.CreateDept(actorContext(1), &orgv1.CreateDeptRequest{Dept: &orgv1.Dept{DeptName: "x"}}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("err = %v, want errServiceFailed", err)
	}
}

func TestGetDept(t *testing.T) {
	svc := &fakeDeptService{get: &orgentity.Dept{ID: 5, DeptName: "市场部", Phone: "13800000000", CreatedAt: time.Unix(0, 0)}}
	app, _ := newDeptApp(svc, new(fakeDeptUsage))
	got, err := app.GetDept(context.Background(), &orgv1.GetDeptRequest{DeptId: 5})
	if err != nil {
		t.Fatalf("GetDept: %v", err)
	}
	if got.DeptId != 5 || got.DeptName != "市场部" || got.Phone != "13800000000" || got.CreatedAt.AsTime().Unix() != 0 {
		t.Fatalf("got = %+v", got)
	}
}

func TestListDepts_BuildsTreeFromFlatList(t *testing.T) {
	svc := &fakeDeptService{list: []*orgentity.Dept{
		{ID: 1, ParentID: 0, DeptName: "root", OrderNum: 1},
		{ID: 3, ParentID: 1, DeptName: "b", OrderNum: 2},
		{ID: 2, ParentID: 1, DeptName: "a", OrderNum: 1},
		{ID: 4, ParentID: 3, DeptName: "b1", OrderNum: 1},
	}}
	app, _ := newDeptApp(svc, new(fakeDeptUsage))

	tree, err := app.ListDepts(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDepts: %v", err)
	}
	if len(tree.Depts) != 1 {
		t.Fatalf("roots = %d, want 1", len(tree.Depts))
	}
	root := tree.Depts[0]
	if len(root.Children) != 2 || root.Children[0].DeptId != 2 || root.Children[1].DeptId != 3 {
		t.Fatalf("children = %+v, want [2 3] (order_num)", root.Children)
	}
	if len(root.Children[1].Children) != 1 || root.Children[1].Children[0].DeptId != 4 {
		t.Fatalf("grandchildren = %+v, want [4]", root.Children[1].Children)
	}
}

func TestListDeptTree_MapsSelectShape(t *testing.T) {
	svc := &fakeDeptService{list: []*orgentity.Dept{
		{ID: 1, ParentID: 0, DeptName: "root", OrderNum: 1},
		{ID: 2, ParentID: 1, DeptName: "a", Status: 1},
	}}
	app, _ := newDeptApp(svc, new(fakeDeptUsage))

	tree, err := app.ListDeptTree(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListDeptTree: %v", err)
	}
	if len(tree.Options) != 1 || tree.Options[0].Label != "root" || tree.Options[0].Disabled {
		t.Fatalf("root option = %+v", tree.Options[0])
	}
	child := tree.Options[0].Children[0]
	if child.DeptId != 2 || !child.Disabled {
		t.Fatalf("child option = %+v, want disabled id 2", child)
	}
}

func TestUpdateDept_AppliesMaskOntoCurrent(t *testing.T) {
	svc := &fakeDeptService{
		get:    &orgentity.Dept{ID: 2, ParentID: 1, DeptName: "a", OrderNum: 1, Phone: "138", Status: 0},
		update: &orgentity.Dept{ID: 2, DeptName: "a2"},
	}
	app, tx := newDeptApp(svc, new(fakeDeptUsage))

	got, err := app.UpdateDept(actorContext(9), &orgv1.UpdateDeptRequest{
		Dept:       &orgv1.Dept{DeptId: 2, DeptName: "a2"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"dept_name"}},
	})
	if err != nil {
		t.Fatalf("UpdateDept: %v", err)
	}
	if got.DeptId != 2 {
		t.Fatalf("DeptId = %d, want 2", got.DeptId)
	}
	if tx.began != 1 || svc.updateCalled != 1 {
		t.Fatalf("tx began %d, update called %d, want 1/1", tx.began, svc.updateCalled)
	}
	in := svc.updateInput
	if in.DeptName != "a2" {
		t.Fatalf("DeptName = %q, want a2 (masked)", in.DeptName)
	}
	if in.ParentID != 1 || in.Phone != "138" || in.OrderNum != 1 {
		t.Fatalf("unmasked fields drifted: %+v", in)
	}
	if in.UpdateBy != 9 {
		t.Fatalf("UpdateBy = %d, want 9", in.UpdateBy)
	}
	if in.CreateBy != 0 {
		t.Fatalf("CreateBy = %d, want 0 (update never touches it)", in.CreateBy)
	}
}

func TestUpdateDept_PropagatesServiceError(t *testing.T) {
	svc := &fakeDeptService{
		get:       &orgentity.Dept{ID: 2, DeptName: "a"},
		updateErr: errServiceFailed,
	}
	app, _ := newDeptApp(svc, new(fakeDeptUsage))
	_, err := app.UpdateDept(actorContext(1), &orgv1.UpdateDeptRequest{
		Dept:       &orgv1.Dept{DeptId: 2, DeptName: "x"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"dept_name"}},
	})
	if !errors.Is(err, errServiceFailed) {
		t.Fatalf("err = %v, want errServiceFailed", err)
	}
}

func TestDeleteDept_PassesIDActorAndTx(t *testing.T) {
	svc := &fakeDeptService{}
	app, tx := newDeptApp(svc, new(fakeDeptUsage))

	if _, err := app.DeleteDept(actorContext(7), &orgv1.DeleteDeptRequest{DeptId: 3}); err != nil {
		t.Fatalf("DeleteDept: %v", err)
	}
	if svc.deleteCalled != 1 || tx.began != 1 {
		t.Fatalf("delete called %d, tx began %d, want 1/1", svc.deleteCalled, tx.began)
	}
	if svc.deleteID != 3 || svc.deleteActor != 7 {
		t.Fatalf("delete id/actor = %d/%d, want 3/7", svc.deleteID, svc.deleteActor)
	}
	if !inTx(svc.deleteCtx) {
		t.Fatal("Delete ran outside the transaction ctx")
	}
}

func TestDeleteDept_PropagatesServiceError(t *testing.T) {
	svc := &fakeDeptService{deleteErr: errServiceFailed}
	app, _ := newDeptApp(svc, new(fakeDeptUsage))
	if _, err := app.DeleteDept(actorContext(1), &orgv1.DeleteDeptRequest{DeptId: 3}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("err = %v, want errServiceFailed", err)
	}
}

func TestDeleteDept_UserGuardRefusesFirst(t *testing.T) {
	svc := &fakeDeptService{}
	usage := &fakeDeptUsage{has: true}
	app, tx := newDeptApp(svc, usage)

	_, err := app.DeleteDept(actorContext(1), &orgv1.DeleteDeptRequest{DeptId: 3})
	if !errors.Is(err, orgdomain.ErrDeptHasUsers) {
		t.Fatalf("err = %v, want ErrDeptHasUsers", err)
	}
	if svc.deleteCalled != 0 || tx.began != 1 {
		t.Fatalf("delete called %d, tx began %d, want 0/1 (guard ran, delete did not)", svc.deleteCalled, tx.began)
	}
	if len(usage.calls) != 1 || usage.calls[0] != 3 {
		t.Fatalf("usage calls = %v, want [3]", usage.calls)
	}
}

func TestDeleteDept_UserGuardErrorPropagates(t *testing.T) {
	svc := &fakeDeptService{}
	usage := &fakeDeptUsage{err: errServiceFailed}
	app, _ := newDeptApp(svc, usage)
	if _, err := app.DeleteDept(actorContext(1), &orgv1.DeleteDeptRequest{DeptId: 3}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("err = %v, want errServiceFailed", err)
	}
}

func TestUpdateDept_DisablingWithUsersRefused(t *testing.T) {
	svc := &fakeDeptService{
		get:    &orgentity.Dept{ID: 2, ParentID: 1, DeptName: "a", Status: 0},
		update: &orgentity.Dept{ID: 2, DeptName: "a", Status: 1},
	}
	usage := &fakeDeptUsage{has: true}
	app, _ := newDeptApp(svc, usage)

	_, err := app.UpdateDept(actorContext(1), &orgv1.UpdateDeptRequest{
		Dept:       &orgv1.Dept{DeptId: 2, Status: 1},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	if !errors.Is(err, orgdomain.ErrDeptHasUsers) {
		t.Fatalf("err = %v, want ErrDeptHasUsers", err)
	}
	if svc.updateCalled != 0 {
		t.Fatal("update ran despite the user guard")
	}
}

func TestUpdateDept_NotDisablingSkipsUserGuard(t *testing.T) {
	svc := &fakeDeptService{
		get:    &orgentity.Dept{ID: 2, ParentID: 1, DeptName: "a", Status: 0},
		update: &orgentity.Dept{ID: 2, DeptName: "a2", Status: 0},
	}
	usage := &fakeDeptUsage{has: true} // would refuse — must not be asked
	app, _ := newDeptApp(svc, usage)

	if _, err := app.UpdateDept(actorContext(1), &orgv1.UpdateDeptRequest{
		Dept:       &orgv1.Dept{DeptId: 2, DeptName: "a2"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"dept_name"}},
	}); err != nil {
		t.Fatalf("UpdateDept: %v", err)
	}
	if len(usage.calls) != 0 {
		t.Fatalf("usage calls = %v, want none", usage.calls)
	}
}
