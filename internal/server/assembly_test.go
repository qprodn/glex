package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"glex/internal/biz"
	"glex/internal/conf"
	"glex/internal/service"

	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
)

type assemblyAuthenticator struct {
	panicOnAuthenticate bool
}

func (a assemblyAuthenticator) Authenticate(context.Context, string) (*biz.Principal, error) {
	if a.panicOnAuthenticate {
		panic("assembly authenticator panic")
	}
	return biz.NewPrincipal(42, "alice", "session-id", nil, []string{"*:*:*"}), nil
}

func TestProductionHTTPRegistersAuthAndOnlineSessionServices(t *testing.T) {
	server := newProductionHTTPTestServer(t, assemblyAuthenticator{})
	routes := make(map[string]bool)
	if err := server.WalkRoute(func(route kratoshttp.RouteInfo) error {
		routes[route.Method+" "+route.Path] = true
		return nil
	}); err != nil {
		t.Fatalf("WalkRoute: %v", err)
	}

	for _, route := range []string{
		"POST /auth/login",
		"POST /auth/logout",
		"GET /monitor/online/list",
		"DELETE /monitor/online/{token_id}",
		"GET /monitor/online",
		"DELETE /monitor/online/myself/{token_id}",
		"GET /system/user/getInfo",
		"GET /system/menu/getRouters",
	} {
		if !routes[route] {
			t.Errorf("production HTTP route %q is not registered", route)
		}
	}
}

func TestProductionGRPCDoesNotExposeAuthServicesWithoutEnforcement(t *testing.T) {
	server := NewGRPCServer(
		&conf.Server{Grpc: &conf.Server_GRPC{}},
		service.NewUserService(nil),
		service.NewMenuService(nil),
		service.NewRoleService(nil),
	)
	services := server.GetServiceInfo()
	for _, serviceName := range []string{
		"system.auth.v1.AuthService",
		"system.auth.v1.OnlineSessionService",
	} {
		if _, ok := services[serviceName]; ok {
			t.Errorf("unprotected gRPC service %q is registered", serviceName)
		}
	}
}

func TestProductionHTTPMiddlewareOrder(t *testing.T) {
	t.Run("auth runs before validation", func(t *testing.T) {
		server := newProductionHTTPTestServer(t, assemblyAuthenticator{})
		request := httptest.NewRequest(http.MethodPost, "/system/user", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("missing credential with invalid request = %d, want 401; body = %s", response.Code, response.Body.String())
		}
	})

	t.Run("recovery wraps auth", func(t *testing.T) {
		server := newProductionHTTPTestServer(t, assemblyAuthenticator{panicOnAuthenticate: true})
		request := httptest.NewRequest(http.MethodGet, "/system/user", nil)
		request.Header.Set("Authorization", "Bearer test-credential")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("auth panic = %d, want recovery 500; body = %s", response.Code, response.Body.String())
		}
	})
}

func newProductionHTTPTestServer(t *testing.T, authenticator biz.Authenticator) *kratoshttp.Server {
	t.Helper()
	server, err := NewHTTPServer(
		&conf.Server{Http: &conf.Server_HTTP{}},
		service.NewUserService(nil),
		service.NewMenuService(nil),
		service.NewRoleService(nil),
		service.NewAuthService(nil),
		service.NewOnlineSessionService(nil),
		authenticator,
	)
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}
	return server
}
