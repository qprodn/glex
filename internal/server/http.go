package server

import (
	authv1 "glex/api/system/auth/v1"
	menuv1 "glex/api/system/menu/v1"
	rolev1 "glex/api/system/role/v1"
	userv1 "glex/api/system/user/v1"
	"glex/internal/biz"
	"glex/internal/conf"
	"glex/internal/service"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type httpServiceRegistration struct {
	descriptor protoreflect.ServiceDescriptor
	register   func(*http.Server)
}

func httpServiceRegistrations(user *service.UserService, menu *service.MenuService, role *service.RoleService, auth *service.AuthService) []httpServiceRegistration {
	return []httpServiceRegistration{
		{
			descriptor: userv1.File_system_user_v1_user_proto.Services().ByName("UserService"),
			register:   func(server *http.Server) { userv1.RegisterUserServiceHTTPServer(server, user) },
		},
		{
			descriptor: menuv1.File_system_menu_v1_menu_proto.Services().ByName("MenuService"),
			register:   func(server *http.Server) { menuv1.RegisterMenuServiceHTTPServer(server, menu) },
		},
		{
			descriptor: rolev1.File_system_role_v1_role_proto.Services().ByName("RoleService"),
			register:   func(server *http.Server) { rolev1.RegisterRoleServiceHTTPServer(server, role) },
		},
		{
			descriptor: authv1.File_system_auth_v1_auth_proto.Services().ByName("AuthService"),
			register:   func(server *http.Server) { authv1.RegisterAuthServiceHTTPServer(server, auth) },
		},
	}
}

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, user *service.UserService, menu *service.MenuService, role *service.RoleService, auth *service.AuthService, authenticator biz.Authenticator) (*http.Server, error) {
	registrations := httpServiceRegistrations(user, menu, role, auth)
	descriptors := make([]protoreflect.ServiceDescriptor, len(registrations))
	for i, registration := range registrations {
		descriptors[i] = registration.descriptor
	}
	policies, err := newAuthPolicyRegistry(descriptors...)
	if err != nil {
		return nil, err
	}
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			authMiddleware(authenticator, policies),
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
	for _, registration := range registrations {
		registration.register(srv)
	}
	return srv, nil
}
