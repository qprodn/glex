package access

import (
	"context"
	"testing"

	accessdomain "glex/internal/domain/access"
	authndomain "glex/internal/domain/authn"

	"google.golang.org/protobuf/types/known/emptypb"
)

type currentRouteMenuUsecase struct {
	menuUsecase
	trees [][]*accessdomain.Menu
	ids   []int64
}

func (u *currentRouteMenuUsecase) ListUserRouteTree(_ context.Context, systemUserID int64) ([]*accessdomain.Menu, error) {
	u.ids = append(u.ids, systemUserID)
	index := len(u.ids) - 1
	if index >= len(u.trees) {
		index = len(u.trees) - 1
	}
	return u.trees[index], nil
}

func TestGetRoutersCallsUsecaseEveryTimeAndProjectsRouteMetadata(t *testing.T) {
	uc := &currentRouteMenuUsecase{trees: [][]*accessdomain.Menu{
		{
			{ID: 1, ParentID: 0, OrderNum: 1, MenuName: "System", Path: "system", MenuType: accessdomain.MenuTypeDirectory, IsCache: 1, Children: []*accessdomain.Menu{
				{ID: 2, ParentID: 1, OrderNum: 1, MenuName: "Users", Path: "users", Component: "system/user/index", QueryParam: `{"tab":"active"}`, MenuType: accessdomain.MenuTypeMenu, Visible: 1, IsCache: 0, Icon: "user", ActiveMenu: "/system/users"},
			}},
		},
		{},
	}}
	service := newMenuApplication(uc, testLogger())
	ctx := authndomain.WithPrincipal(t.Context(), authndomain.NewPrincipal(42, "alice", "session-id", nil, []string{"unrelated:permission"}))

	first, err := service.GetRouters(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("first GetRouters: %v", err)
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

	second, err := service.GetRouters(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("second GetRouters: %v", err)
	}
	if len(second.GetRouters()) != 0 || len(uc.ids) != 2 || uc.ids[0] != 42 || uc.ids[1] != 42 {
		t.Fatalf("GetRouters did not call usecase per request: routes=%v ids=%v", second.GetRouters(), uc.ids)
	}
}

func TestGetRoutersUsesRuoYiPathComponentAndFrameSemantics(t *testing.T) {
	uc := &currentRouteMenuUsecase{trees: [][]*accessdomain.Menu{{
		{ID: 1, ParentID: 0, MenuName: "Root directory", Path: "system", MenuType: accessdomain.MenuTypeDirectory, IsFrame: 0, Children: []*accessdomain.Menu{
			{ID: 2, ParentID: 1, MenuName: "Nested directory", Path: "nested", MenuType: accessdomain.MenuTypeDirectory, IsFrame: 0},
			{ID: 4, ParentID: 1, MenuName: "Docs", Path: "https://docs.example.com", MenuType: accessdomain.MenuTypeMenu, IsFrame: 0},
		}},
		{ID: 3, ParentID: 0, MenuName: "Dashboard", Path: "dashboard", Component: "dashboard/index", MenuType: accessdomain.MenuTypeMenu, IsFrame: 0},
	}}}
	service := newMenuApplication(uc, testLogger())
	ctx := authndomain.WithPrincipal(t.Context(), authndomain.NewPrincipal(42, "alice", "session-id", nil, nil))

	reply, err := service.GetRouters(ctx, &emptypb.Empty{})
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
	menuService := newMenuApplication(&currentRouteMenuUsecase{}, testLogger())
	if _, err := menuService.GetRouters(t.Context(), &emptypb.Empty{}); err != authndomain.ErrCredentialMissing {
		t.Fatalf("GetRouters without Principal = %v", err)
	}
}
