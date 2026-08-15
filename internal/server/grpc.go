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
	"github.com/go-kratos/kratos/v3/transport/grpc"
)

// NewGRPCServer new a gRPC server.
func NewGRPCServer(c *conf.Server, user *service.UserService, menu *service.MenuService, role *service.RoleService, auth *service.AuthService) *grpc.Server {
	var opts = []grpc.ServerOption{
		grpc.Middleware(
			recovery.Recovery(),
			validate.Middleware(),
		),
	}
	if c.Grpc.Network != "" {
		opts = append(opts, grpc.Network(c.Grpc.Network))
	}
	if c.Grpc.Addr != "" {
		opts = append(opts, grpc.Address(c.Grpc.Addr))
	}
	if c.Grpc.Timeout != nil {
		opts = append(opts, grpc.Timeout(c.Grpc.Timeout.AsDuration()))
	}
	srv := grpc.NewServer(opts...)
	userv1.RegisterUserServiceServer(srv, user)
	menuv1.RegisterMenuServiceServer(srv, menu)
	rolev1.RegisterRoleServiceServer(srv, role)
	authv1.RegisterAuthServiceServer(srv, auth)
	return srv
}
