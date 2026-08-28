package access

import (
	"context"
	"testing"

	accessentity "glex/internal/domain/access/entity"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"

	"google.golang.org/protobuf/types/known/emptypb"
)

// routerFixture builds a superadmin routeAccess over the given flat menus so
// GetRouters renders every node.
func routerFixture(menus ...*accessentity.Menu) *accessentity.MenuRouteAccess {
	return &accessentity.MenuRouteAccess{RoleKeys: []string{"superadmin"}, ActiveMenus: menus}
}

func TestGetRoutersProjectsRouteMetadataPerRequest(t *testing.T) {
	svc := &fakeMenuService{routeAccess: routerFixture(
		&accessentity.Menu{ID: 1, ParentID: 0, OrderNum: 1, MenuName: "System", Path: "system", MenuType: accessentity.MenuTypeDirectory, IsCache: 1},
		&accessentity.Menu{ID: 2, ParentID: 1, OrderNum: 1, MenuName: "Users", Path: "users", Component: "system/user/index", QueryParam: `{"tab":"active"}`, MenuType: accessentity.MenuTypeMenu, Visible: 1, IsCache: 0, Icon: "user", ActiveMenu: "/system/users"},
	)}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(42, "alice", "session-id", nil, []string{"unrelated:permission"}))

	first, err := app.GetRouters(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("first GetRouters: %v", err)
	}
	// The route data is loaded for the authenticated System User.
	if svc.routeUserID != 42 {
		t.Fatalf("service saw system user %d, want the principal's 42", svc.routeUserID)
	}
	if len(first.GetRouters()) != 1 || len(first.GetRouters()[0].GetChildren()) != 1 {
		t.Fatalf("first route tree = %#v", first)
	}
	root := first.GetRouters()[0]
	child := root.GetChildren()[0]
	if root.GetName() != "System1" || root.GetPath() != "/system" || root.GetComponent() != "Layout" || !root.GetAlwaysShow() || root.GetRedirect() != "noRedirect" {
		t.Fatalf("directory Router = %#v", root)
	}
	if child.GetName() != "Users2" || !child.GetHidden() || child.GetComponent() != "system/user/index" || child.GetQuery() != `{"tab":"active"}` {
		t.Fatalf("menu Router = %#v", child)
	}
	if !child.GetMeta().GetNoCache() || child.GetMeta().GetTitle() != "Users" || child.GetMeta().GetIcon() != "user" || child.GetMeta().GetActiveMenu() != "/system/users" {
		t.Fatalf("Router meta = %#v", child.GetMeta())
	}

	// The route tree is resolved fresh from the service on every request.
	svc.routeAccess = nil
	second, err := app.GetRouters(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("second GetRouters: %v", err)
	}
	if len(second.GetRouters()) != 0 {
		t.Fatalf("second GetRouters must reflect the fresh (empty) store: %v", second.GetRouters())
	}
}

func TestGetRoutersUsesRuoYiPathComponentAndFrameSemantics(t *testing.T) {
	svc := &fakeMenuService{routeAccess: routerFixture(
		&accessentity.Menu{ID: 1, ParentID: 0, OrderNum: 1, MenuName: "Root directory", Path: "system", MenuType: accessentity.MenuTypeDirectory, IsFrame: 0},
		&accessentity.Menu{ID: 2, ParentID: 1, OrderNum: 1, MenuName: "Nested directory", Path: "nested", MenuType: accessentity.MenuTypeDirectory, IsFrame: 0},
		&accessentity.Menu{ID: 4, ParentID: 1, OrderNum: 2, MenuName: "Docs", Path: "https://docs.example.com", MenuType: accessentity.MenuTypeMenu, IsFrame: 0},
		&accessentity.Menu{ID: 3, ParentID: 0, OrderNum: 2, MenuName: "Dashboard", Path: "dashboard", Component: "dashboard/index", MenuType: accessentity.MenuTypeMenu, IsFrame: 0},
	)}
	app, _ := newMenuApp(svc, new(fakeSessionRevoker))
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(42, "alice", "session-id", nil, nil))

	reply, err := app.GetRouters(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetRouters: %v", err)
	}
	root := reply.GetRouters()[0]
	if root.GetPath() != "/system" || root.GetComponent() != "Layout" {
		t.Fatalf("root Directory = %#v", root)
	}
	nested := root.GetChildren()[0]
	if nested.GetPath() != "nested" || nested.GetComponent() != "ParentView" {
		t.Fatalf("nested Directory = %#v", nested)
	}
	inner := root.GetChildren()[1]
	if inner.GetName() != "Docs/example/com4" || inner.GetPath() != "docs/example/com" || inner.GetComponent() != "InnerLink" || inner.GetMeta().GetLink() != "https://docs.example.com" {
		t.Fatalf("nested inner link = %#v", inner)
	}
	frame := reply.GetRouters()[1]
	if frame.GetName() != "3" || frame.GetPath() != "/" || frame.GetComponent() != "Layout" || frame.GetMeta() != nil || len(frame.GetChildren()) != 1 {
		t.Fatalf("root Menu frame = %#v", frame)
	}
	child := frame.GetChildren()[0]
	if child.GetName() != "Dashboard3" || child.GetPath() != "dashboard" || child.GetComponent() != "dashboard/index" {
		t.Fatalf("root Menu frame child = %#v", child)
	}
}

func TestGetRoutersRequiresPrincipal(t *testing.T) {
	app, _ := newMenuApp(new(fakeMenuService), new(fakeSessionRevoker))
	if _, err := app.GetRouters(context.Background(), &emptypb.Empty{}); err != authndomain.ErrCredentialMissing {
		t.Fatalf("GetRouters without Principal = %v", err)
	}
}
