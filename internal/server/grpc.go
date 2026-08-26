package server

import (
	"log/slog"

	accessv1 "glex/api/access/v1"
	userv1 "glex/api/user/v1"
	accessapp "glex/internal/application/access"
	userapp "glex/internal/application/user"
	"glex/internal/conf"
	"glex/pkg/middleware/requestid"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/logging"
	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/transport/grpc"
)

// NewGRPCServer new a gRPC server.
func NewGRPCServer(c *conf.Server, user *userapp.UserApplication, menu *accessapp.MenuApplication, role *accessapp.RoleApplication, logger *slog.Logger) *grpc.Server {
	var opts = []grpc.ServerOption{
		grpc.Middleware(
			requestid.Server(requestid.WithLogger(logger)),
			logging.Server(logger),
			recovery.Recovery(recovery.WithLogger(logger)),
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
	accessv1.RegisterMenuServiceServer(srv, menu)
	accessv1.RegisterRoleServiceServer(srv, role)
	return srv
}
