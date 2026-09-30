package access

import (
	"context"
	"errors"
	"testing"

	accessv1 "glex/api/access/v1"
	accessentity "glex/internal/domain/access/entity"
	accessservice "glex/internal/domain/access/service"

	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fakeMenuService is a hand-rolled accessservice.Menu fake for the
// application tests: it records every call and returns the configured result,
// so the tests exercise orchestration only — transaction wrapping, revocation,
// DTO conversion, field-mask application — never the domain rules behind the
// interface.
type fakeMenuService struct {
	// Configured returns.
	get         *accessentity.Menu
	list        []*accessentity.Menu
	routeAccess *accessentity.MenuRouteAccess
	create      *accessentity.Menu
	update      *accessentity.MenuUpdate
	deleteUsers []int64
	getErr      error
	listErr     error
	routeErr    error
	createErr   error
	updateErr   error
	deleteErr   error

	// Recorded calls.
	createCalled int
	createCtx    context.Context
	createInput  *accessentity.Menu
	updateCalled int
	updateCtx    context.Context
	updateInput  *accessentity.Menu
	deleteCalled int
	deleteCtx    context.Context
	deleteID     int64
	deleteActor  int64
	routeUserID  int64
}

func (f *fakeMenuService) Get(_ context.Context, _ int64) (*accessentity.Menu, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.get, nil
}

func (f *fakeMenuService) List(_ context.Context, _ accessservice.ListMenusRequest) ([]*accessentity.Menu, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakeMenuService) LoadRouteAccess(_ context.Context, systemUserID int64) (*accessentity.MenuRouteAccess, error) {
	f.routeUserID = systemUserID
	if f.routeErr != nil {
		return nil, f.routeErr
	}
	return f.routeAccess, nil
}

func (f *fakeMenuService) Create(ctx context.Context, m *accessentity.Menu) (*accessentity.Menu, error) {
	f.createCalled++
	f.createCtx, f.createInput = ctx, m
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.create, nil
}

func (f *fakeMenuService) Update(ctx context.Context, m *accessentity.Menu) (*accessentity.MenuUpdate, error) {
	f.updateCalled++
	f.updateCtx, f.updateInput = ctx, m
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.update, nil
}

func (f *fakeMenuService) Delete(ctx context.Context, req accessservice.DeleteMenuRequest) ([]int64, error) {
	f.deleteCalled++
	f.deleteCtx, f.deleteID, f.deleteActor = ctx, req.ID, req.ActorID
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return f.deleteUsers, nil
}

func newMenuApp(svc *fakeMenuService, revoker *fakeSessionRevoker) (*MenuApplication, *fakeTransaction) {
	tx := newFakeTransaction()
	return NewMenuApplication(svc, tx, revoker, testLogger()), tx
}

func TestCreateMenu_PassesEntityWithActorAndTx(t *testing.T) {
	svc := &fakeMenuService{create: &accessentity.Menu{ID: 7, MenuName: "sys", MenuType: accessentity.MenuTypeDirectory}}
	app, tx := newMenuApp(svc, new(fakeSessionRevoker))

	got, err := app.CreateMenu(actorContext(42), &accessv1.CreateMenuRequest{Menu: &accessv1.Menu{
		MenuName: "sys", ParentId: 0, MenuType: accessv1.MenuType_MENU_TYPE_DIRECTORY, OrderNum: 1, Path: "system",
	}})
	if err != nil {
		t.Fatalf("create menu: %v", err)
	}
	if svc.createCtx != tx.bodyCtx {
		t.Fatal("service Create must see the ctx the transaction body received")
	}
	in := svc.createInput
	if in.MenuName != "sys" || in.ParentID != 0 || in.MenuType != accessentity.MenuTypeDirectory || in.OrderNum != 1 || in.Path != "system" {
		t.Fatalf("service input = %+v", in)
	}
	if in.CreateBy != 42 || in.UpdateBy != 42 {
		t.Fatalf("audit fields = %d/%d, want the acting principal 42", in.CreateBy, in.UpdateBy)
	}
	// The reply is the service result converted to the DTO.
	if got.GetMenuId() != 7 || got.GetMenuName() != "sys" || got.GetMenuType() != accessv1.MenuType_MENU_TYPE_DIRECTORY {
		t.Fatalf("reply = %#v", got)
	}
}

func TestCreateMenu_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeMenuService{createErr: errServiceFailed}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))

	if _, err := app.CreateMenu(context.Background(), &accessv1.CreateMenuRequest{Menu: &accessv1.Menu{MenuName: "sys", MenuType: accessv1.MenuType_MENU_TYPE_DIRECTORY}}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestGetMenu_ConvertsDTO(t *testing.T) {
	svc := &fakeMenuService{get: &accessentity.Menu{
		ID: 7, MenuName: "sys", ParentID: 0, OrderNum: 1, Path: "system", Component: "system/user/index",
		MenuType: accessentity.MenuTypeMenu, Perms: "system:user:list", Icon: "user",
	}}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))

	got, err := app.GetMenu(context.Background(), &accessv1.GetMenuRequest{MenuId: 7})
	if err != nil {
		t.Fatalf("get menu: %v", err)
	}
	if got.GetMenuId() != 7 || got.GetMenuName() != "sys" || got.GetParentId() != 0 || got.GetOrderNum() != 1 ||
		got.GetPath() != "system" || got.GetComponent() != "system/user/index" ||
		got.GetMenuType() != accessv1.MenuType_MENU_TYPE_MENU || got.GetPerms() != "system:user:list" || got.GetIcon() != "user" {
		t.Fatalf("reply = %#v", got)
	}
}

func TestGetMenu_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeMenuService{getErr: errServiceFailed}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))

	if _, err := app.GetMenu(context.Background(), &accessv1.GetMenuRequest{MenuId: 7}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestListMenus_BuildsTreeFromFlatServiceList(t *testing.T) {
	svc := &fakeMenuService{list: []*accessentity.Menu{
		{ID: 1, ParentID: 0, OrderNum: 1, MenuName: "root", MenuType: accessentity.MenuTypeDirectory},
		{ID: 2, ParentID: 1, OrderNum: 1, MenuName: "child", MenuType: accessentity.MenuTypeMenu},
	}}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))

	tree, err := app.ListMenus(context.Background(), &accessv1.ListMenusRequest{})
	if err != nil {
		t.Fatalf("list tree: %v", err)
	}
	// The handler assembles the flat service result into a forest before
	// converting it.
	if len(tree.GetMenus()) != 1 || tree.GetMenus()[0].GetMenuId() != 1 {
		t.Fatalf("expected single root id 1, got %+v", tree.GetMenus())
	}
	children := tree.GetMenus()[0].GetChildren()
	if len(children) != 1 || children[0].GetMenuId() != 2 {
		t.Fatalf("expected child id 2 under root, got %+v", children)
	}
}

func TestListMenus_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeMenuService{listErr: errServiceFailed}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))

	if _, err := app.ListMenus(context.Background(), &accessv1.ListMenusRequest{}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestUpdateMenu_AppliesFieldMaskBeforeService(t *testing.T) {
	svc := &fakeMenuService{
		get: &accessentity.Menu{ID: 1, MenuName: "a", Path: "p", MenuType: accessentity.MenuTypeMenu},
		update: &accessentity.MenuUpdate{
			Menu:     &accessentity.Menu{ID: 1, MenuName: "b", Path: "p", MenuType: accessentity.MenuTypeMenu},
			Previous: &accessentity.Menu{ID: 1, MenuName: "a", Path: "p", MenuType: accessentity.MenuTypeMenu},
		},
	}
	app, tx := newMenuApp(svc, new(fakeSessionRevoker))

	got, err := app.UpdateMenu(actorContext(42), &accessv1.UpdateMenuRequest{
		Menu:       &accessv1.Menu{MenuId: 1, MenuName: "b"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"menu_name"}},
	})
	if err != nil {
		t.Fatalf("update menu: %v", err)
	}
	in := svc.updateInput
	// Only the masked field changed; unmasked fields keep the loaded state.
	if in.MenuName != "b" || in.Path != "p" || in.MenuType != accessentity.MenuTypeMenu {
		t.Fatalf("service input = %+v", in)
	}
	if in.UpdateBy != 42 {
		t.Fatalf("update audit actor = %d, want 42", in.UpdateBy)
	}
	if svc.updateCtx != tx.bodyCtx {
		t.Fatal("service Update must see the ctx the transaction body received")
	}
	if got.GetMenuName() != "b" {
		t.Fatalf("reply = %#v", got)
	}
}

func TestUpdateMenu_RevokesWhenAuthorizationChanged(t *testing.T) {
	current := &accessentity.Menu{ID: 1, MenuName: "Users", Perms: "system:user:list", Status: 0, MenuType: accessentity.MenuTypeMenu}
	changed := &accessentity.Menu{ID: 1, MenuName: "Users", Perms: "system:user:edit", Status: 0, MenuType: accessentity.MenuTypeMenu}
	svc := &fakeMenuService{
		get:    current,
		update: &accessentity.MenuUpdate{Menu: changed, Previous: current, AffectedUserIDs: []int64{3, 4}},
	}
	revoker := new(fakeSessionRevoker)
	app, _ := newMenuApp(svc, revoker)

	if _, err := app.UpdateMenu(context.Background(), &accessv1.UpdateMenuRequest{Menu: &accessv1.Menu{MenuId: 1, Perms: "system:user:edit"}}); err != nil {
		t.Fatalf("update menu: %v", err)
	}
	if len(revoker.requests) != 1 {
		t.Fatalf("revocation requests = %#v, want exactly one", revoker.requests)
	}
	ids := revoker.requests[0].SystemUserIDs
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 4 {
		t.Fatalf("revoked ids = %v, want the affected users [3 4]", ids)
	}
}

func TestUpdateMenu_NoRevocationWhenAuthorizationUnchanged(t *testing.T) {
	current := &accessentity.Menu{ID: 1, MenuName: "Users", Perms: "system:user:list", Status: 0, MenuType: accessentity.MenuTypeMenu}
	renamed := &accessentity.Menu{ID: 1, MenuName: "People", Perms: "system:user:list", Status: 0, MenuType: accessentity.MenuTypeMenu}
	svc := &fakeMenuService{
		get:    current,
		update: &accessentity.MenuUpdate{Menu: renamed, Previous: current, AffectedUserIDs: []int64{3}},
	}
	revoker := new(fakeSessionRevoker)
	app, _ := newMenuApp(svc, revoker)

	if _, err := app.UpdateMenu(context.Background(), &accessv1.UpdateMenuRequest{Menu: &accessv1.Menu{MenuId: 1, MenuName: "People"}}); err != nil {
		t.Fatalf("update menu: %v", err)
	}
	if len(revoker.requests) != 0 {
		t.Fatalf("non-authorization change must not revoke, got %#v", revoker.requests)
	}
}

func TestUpdateMenu_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeMenuService{
		get:       &accessentity.Menu{ID: 1, MenuName: "a", MenuType: accessentity.MenuTypeMenu},
		updateErr: errServiceFailed,
	}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))

	if _, err := app.UpdateMenu(context.Background(), &accessv1.UpdateMenuRequest{Menu: &accessv1.Menu{MenuId: 1, MenuName: "a"}}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestDeleteMenu_RevokesAffectedUsers(t *testing.T) {
	svc := &fakeMenuService{deleteUsers: []int64{3, 4}}
	revoker := new(fakeSessionRevoker)
	app, tx := newMenuApp(svc, revoker)

	if _, err := app.DeleteMenu(actorContext(42), &accessv1.DeleteMenuRequest{MenuId: 2}); err != nil {
		t.Fatalf("delete menu: %v", err)
	}
	if svc.deleteID != 2 || svc.deleteActor != 42 {
		t.Fatalf("service saw delete id/actor %d/%d, want 2/42", svc.deleteID, svc.deleteActor)
	}
	if svc.deleteCtx != tx.bodyCtx {
		t.Fatal("service Delete must see the ctx the transaction body received")
	}
	if len(revoker.requests) != 1 || len(revoker.requests[0].SystemUserIDs) != 2 || revoker.requests[0].SystemUserIDs[0] != 3 {
		t.Fatalf("revocation requests = %#v, want one request revoking [3 4]", revoker.requests)
	}
}

func TestDeleteMenu_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeMenuService{deleteErr: errServiceFailed}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))

	if _, err := app.DeleteMenu(context.Background(), &accessv1.DeleteMenuRequest{MenuId: 2}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}
