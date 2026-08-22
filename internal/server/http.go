package server

import (
	authv1 "glex/api/iam/auth/v1"
	menuv1 "glex/api/iam/menu/v1"
	rolev1 "glex/api/iam/role/v1"
	userv1 "glex/api/iam/user/v1"
	iamapp "glex/internal/application/iam"
	"glex/internal/conf"
	"glex/internal/domain/iam"
	authmw "glex/pkg/middleware/auth"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/transport/http"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, user *iamapp.UserApplication, menu *iamapp.MenuApplication, role *iamapp.RoleApplication, auth *iamapp.AuthApplication, online *iamapp.OnlineSessionApplication, authenticator iam.Authenticator) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			authmw.Middleware(authenticatorAdapter{authenticator: authenticator}, authRoutes()),
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
	authv1.RegisterOnlineSessionServiceHTTPServer(srv, online)
	return srv
}
