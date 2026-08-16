package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	menuv1 "glex/api/system/menu/v1"
	userv1 "glex/api/system/user/v1"
	"glex/internal/biz"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/protobuf/types/known/emptypb"
)

type bootstrapUserService struct {
	userv1.UnimplementedUserServiceServer
	calls int
}

func (s *bootstrapUserService) GetInfo(context.Context, *emptypb.Empty) (*userv1.CurrentUserInfo, error) {
	s.calls++
	return &userv1.CurrentUserInfo{
		User:        &userv1.CurrentUser{UserId: 42, UserName: "alice"},
		RoleKeys:    []string{"operator"},
		Permissions: []string{"system:user:list"},
	}, nil
}

type bootstrapMenuService struct {
	menuv1.UnimplementedMenuServiceServer
	calls int
}

func (s *bootstrapMenuService) GetRouters(context.Context, *emptypb.Empty) (*menuv1.RouterTree, error) {
	s.calls++
	return &menuv1.RouterTree{Routers: []*menuv1.Router{{Name: "System1"}}}, nil
}

func TestCurrentUserBootstrapHTTPRoutes(t *testing.T) {
	userService := &bootstrapUserService{}
	menuService := &bootstrapMenuService{}
	principal := biz.NewPrincipal(42, "alice", "session-id", []string{"operator"}, []string{"system:user:list"})
	server := newCurrentUserRouteTestServer(t, principal, userService, menuService)

	infoResponse := routeRequest(t, server, http.MethodGet, "/system/user/getInfo", "", "Bearer route-bearer-secret")
	if infoResponse.Code != http.StatusOK {
		t.Fatalf("GET /system/user/getInfo = %d; body = %s", infoResponse.Code, infoResponse.Body.String())
	}
	var info userv1.CurrentUserInfo
	if err := json.Unmarshal(infoResponse.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode GetInfo: %v", err)
	}
	if info.GetUser().GetUserId() != 42 || strings.Join(info.GetRoleKeys(), ",") != "operator" || strings.Join(info.GetPermissions(), ",") != "system:user:list" {
		t.Fatalf("GetInfo reply = %#v", &info)
	}
	if userService.calls != 1 {
		t.Fatalf("GetInfo handler calls = %d, want 1", userService.calls)
	}

	routerResponse := routeRequest(t, server, http.MethodGet, "/system/menu/getRouters", "", "Bearer route-bearer-secret")
	if routerResponse.Code != http.StatusOK {
		t.Fatalf("GET /system/menu/getRouters = %d; body = %s", routerResponse.Code, routerResponse.Body.String())
	}
	var routers menuv1.RouterTree
	if err := json.Unmarshal(routerResponse.Body.Bytes(), &routers); err != nil {
		t.Fatalf("decode GetRouters: %v", err)
	}
	if len(routers.GetRouters()) != 1 || routers.GetRouters()[0].GetName() != "System1" || menuService.calls != 1 {
		t.Fatalf("GetRouters reply = %#v, handler calls = %d", &routers, menuService.calls)
	}
}

func TestCurrentUserBootstrapHTTPRoutesRequireAuthentication(t *testing.T) {
	server := newCurrentUserRouteTestServer(t, biz.NewPrincipal(42, "alice", "session-id", nil, nil), &bootstrapUserService{}, &bootstrapMenuService{})
	for _, path := range []string{"/system/user/getInfo", "/system/menu/getRouters"} {
		response := routeRequest(t, server, http.MethodGet, path, "", "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s without bearer = %d, want 401; body = %s", path, response.Code, response.Body.String())
		}
	}
}

func newCurrentUserRouteTestServer(t *testing.T, principal *biz.Principal, userService userv1.UserServiceHTTPServer, menuService menuv1.MenuServiceHTTPServer) *kratoshttp.Server {
	t.Helper()
	policies, err := newAuthPolicyRegistry(
		userv1.File_system_user_v1_user_proto.Services().ByName("UserService"),
		menuv1.File_system_menu_v1_menu_proto.Services().ByName("MenuService"),
	)
	if err != nil {
		t.Fatalf("newAuthPolicyRegistry: %v", err)
	}
	server := kratoshttp.NewServer(kratoshttp.Middleware(
		recovery.Recovery(),
		authMiddleware(&routeAuthenticator{principal: principal}, policies),
		validate.Middleware(),
	))
	// Production assembly belongs to issue 09. Registering here exercises the
	// generated HTTP handlers and route precedence without changing it early.
	userv1.RegisterUserServiceHTTPServer(server, userService)
	menuv1.RegisterMenuServiceHTTPServer(server, menuService)
	return server
}
