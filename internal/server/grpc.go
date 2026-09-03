package server

import (
	"log/slog"

	accessv1 "glex/api/access/v1"
	dictv1 "glex/api/dict/v1"
	filev1 "glex/api/file/v1"
	userv1 "glex/api/user/v1"
	accessapp "glex/internal/application/access"
	dictapp "glex/internal/application/dict"
	fileapp "glex/internal/application/file"
	userapp "glex/internal/application/user"
	"glex/internal/conf"
	"glex/pkg/middleware/logging"
	"glex/pkg/middleware/requestid"
	"glex/pkg/validate"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/transport/grpc"
)

// NewGRPCServer new a gRPC server.
func NewGRPCServer(c *conf.Server, user *userapp.UserApplication, menu *accessapp.MenuApplication, role *accessapp.RoleApplication, dict *dictapp.DictApplication, file *fileapp.FileApplication, logger *slog.Logger) *grpc.Server {
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
	dictv1.RegisterDictServiceServer(srv, dict)
	filev1.RegisterFileServiceServer(srv, file)
	return srv
}
