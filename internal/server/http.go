package server

import (
	authv1 "glex/api/system/auth/v1"
	menuv1 "glex/api/system/menu/v1"
	rolev1 "glex/api/system/role/v1"
	userv1 "glex/api/system/user/v1"
	"glex/internal/conf"
	"glex/internal/service"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/transport/http"
)

// NewHTTPServer new an HTTP server.
//
// No auth middleware yet — the template's pkg/auth was removed and will be
// replaced by the real auth module. Endpoints are currently unauthenticated.
func NewHTTPServer(c *conf.Server, user *service.UserService, menu *service.MenuService, role *service.RoleService, auth *service.AuthService) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			validate.Middleware(),
		),
	}
	if c.Http.Network != "" {
		opts = append(opts, http.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, http.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, http.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := http.NewServer(opts...)
	userv1.RegisterUserServiceHTTPServer(srv, user)
	menuv1.RegisterMenuServiceHTTPServer(srv, menu)
	rolev1.RegisterRoleServiceHTTPServer(srv, role)
	authv1.RegisterAuthServiceHTTPServer(srv, auth)
	return srv
}
