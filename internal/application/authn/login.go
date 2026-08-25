package authn

import (
	"context"
	"log/slog"
	"strings"
	"time"

	authv1 "glex/api/iam/auth/v1"
	authndomain "glex/internal/domain/authn"

	"github.com/go-kratos/kratos/v3/transport"
	"google.golang.org/protobuf/types/known/emptypb"
)

// LoginApplication implements the authn span of the proto AuthService: the
// Login/Logout endpoints. The role-assignment span of the same proto service
// stays with iam until the contracts split (ticket 06); server joins the two
// spans at registration.
type LoginApplication struct {
	authv1.UnimplementedAuthServiceServer

	uc     *LoginUsecase
	logger *slog.Logger
}

// NewLoginApplication creates a LoginApplication.
func NewLoginApplication(uc *LoginUsecase, logger *slog.Logger) *LoginApplication {
	return &LoginApplication{
		uc:     uc,
		logger: logger.With(slog.String("component", "application.authn.login")),
	}
}

// Login creates one Login Session and returns its bearer credential exactly
// once. Request metadata is read at this transport boundary and passed to the
// Usecase.
func (s *LoginApplication) Login(ctx context.Context, req *authv1.LoginRequest) (*authv1.LoginReply, error) {
	sourceIP, userAgent := "", ""
	if transporter, ok := transport.FromServerContext(ctx); ok && transporter.RequestHeader() != nil {
		sourceIP = strings.TrimSpace(transporter.RequestHeader().Get("X-Forwarded-For"))
		userAgent = transporter.RequestHeader().Get("User-Agent")
	}
	result, err := s.uc.Login(ctx, &LoginRequest{
		UserName:  req.GetUsername(),
		Password:  req.GetPassword(),
		SourceIP:  sourceIP,
		UserAgent: userAgent,
	})
	if err != nil {
		return nil, err
	}
	return &authv1.LoginReply{
		AccessToken: result.AccessToken,
		ExpireIn:    int64(result.ExpiresIn / time.Second),
	}, nil
}

// Logout succeeds anonymously and otherwise revokes only the current Login
// Session attached by OPTIONAL authentication.
func (s *LoginApplication) Logout(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	principal, _ := authndomain.PrincipalFromContext(ctx)
	if err := s.uc.Logout(ctx, principal); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
