package server

import (
	"log/slog"

	authv1 "glex/api/iam/auth/v1"
	menuv1 "glex/api/iam/menu/v1"
	rolev1 "glex/api/iam/role/v1"
	userv1 "glex/api/iam/user/v1"
	authnapp "glex/internal/application/authn"
	iamapp "glex/internal/application/iam"
	"glex/internal/conf"
	authndomain "glex/internal/domain/authn"
	authmw "glex/pkg/middleware/auth"
	"glex/pkg/middleware/requestid"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/logging"
	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/transport/http"
)

// authServiceServer joins the authn-owned login/logout endpoints with iam's
// role-assignment endpoints behind the one proto AuthService until the
// contracts split (ticket 06). Each span implements its own methods; the
// embedded Unimplemented fallbacks are shadowed by the concrete methods.
type authServiceServer struct {
	*authnapp.LoginApplication
	*iamapp.RoleAssignmentApplication
}

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, user *iamapp.UserApplication, menu *iamapp.MenuApplication, role *iamapp.RoleApplication, assignment *iamapp.RoleAssignmentApplication, login *authnapp.LoginApplication, online *authnapp.OnlineSessionApplication, authenticator authndomain.Authenticator, logger *slog.Logger) *http.Server {
	var opts = []http.ServerOption{
		http.Filter(
			requestid.HTTP(requestid.WithLogger(logger)),
		),
		http.Middleware(
			logging.Server(logger),
			recovery.Recovery(recovery.WithLogger(logger)),
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
	authv1.RegisterAuthServiceHTTPServer(srv, authServiceServer{
		LoginApplication:          login,
		RoleAssignmentApplication: assignment,
	})
	authv1.RegisterOnlineSessionServiceHTTPServer(srv, online)
	return srv
}
