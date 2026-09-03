package authn

import (
	"context"
	"log/slog"

	authnv1 "glex/api/authn/v1"
	authndomain "glex/internal/domain/authn"
	authnservice "glex/internal/domain/authn/service"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// OnlineSessionApplication implements the four RuoYi-compatible Login Session
// operations. All repository access goes through the Online Session domain
// service (ADR 0005); bulk revocation is intentionally absent from the
// endpoints, and authentication policy is enforced by the registered HTTP
// middleware.
type OnlineSessionApplication struct {
	authnv1.UnimplementedOnlineSessionServiceServer

	svc    authnservice.OnlineSession
	logger *slog.Logger
}

// NewOnlineSessionApplication creates an OnlineSessionApplication.
func NewOnlineSessionApplication(svc authnservice.OnlineSession, logger *slog.Logger) *OnlineSessionApplication {
	return &OnlineSessionApplication{
		svc:    svc,
		logger: logger.With(slog.String("component", "application.online_session")),
	}
}

// ListOnlineSessions lists active Login Sessions across all System Users.
func (s *OnlineSessionApplication) ListOnlineSessions(ctx context.Context, req *authnv1.ListOnlineSessionsRequest) (*authnv1.OnlineSessionSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("ipaddr", filtering.TypeString),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.list_online_sessions", err)
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.list_online_sessions", err)
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.list_online_sessions", err)
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.list_online_sessions", err)
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		orderBy.Fields = []ordering.Field{{Path: "login_time", Desc: true}, {Path: "token_id"}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	sessions, err := s.svc.List(ctx, filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.list_online_sessions", err)
		return nil, err
	}
	set := &authnv1.OnlineSessionSet{Sessions: make([]*authnv1.OnlineSession, 0, len(sessions))}
	for _, session := range sessions {
		set.Sessions = append(set.Sessions, &authnv1.OnlineSession{
			TokenId:    session.ID,
			UserName:   session.UserName,
			DeviceType: session.DeviceType,
			Ipaddr:     session.IPAddress,
			Browser:    session.Browser,
			Os:         session.OS,
			LoginTime:  timestamppb.New(session.LoginTime),
		})
	}
	if len(sessions) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// KickoutOnlineSession revokes one Session ID for an administrator.
func (s *OnlineSessionApplication) KickoutOnlineSession(ctx context.Context, req *authnv1.OnlineSessionRequest) (*emptypb.Empty, error) {
	if err := s.svc.Kickout(ctx, req.GetTokenId()); err != nil {
		logx.Propagate(ctx, s.logger, "authn.kickout_online_session", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListMyOnlineSessions lists only the current System User's Login Sessions.
func (s *OnlineSessionApplication) ListMyOnlineSessions(ctx context.Context, _ *emptypb.Empty) (*authnv1.OnlineSessionSet, error) {
	principal, ok := authnservice.PrincipalFromContext(ctx)
	if !ok {
		err := authndomain.ErrCredentialMissing
		logx.Propagate(ctx, s.logger, "authn.list_my_online_sessions", err)
		return nil, err
	}
	sessions, err := s.svc.ListBySystemUser(ctx, principal.SystemUserID())
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.list_my_online_sessions", err)
		return nil, err
	}
	set := &authnv1.OnlineSessionSet{Sessions: make([]*authnv1.OnlineSession, 0, len(sessions))}
	for _, session := range sessions {
		set.Sessions = append(set.Sessions, &authnv1.OnlineSession{
			TokenId:    session.ID,
			UserName:   session.UserName,
			DeviceType: session.DeviceType,
			Ipaddr:     session.IPAddress,
			Browser:    session.Browser,
			Os:         session.OS,
			LoginTime:  timestamppb.New(session.LoginTime),
		})
	}
	return set, nil
}

// DeleteMyOnlineSession revokes one Session only after an atomic ownership
// check against the current System User.
func (s *OnlineSessionApplication) DeleteMyOnlineSession(ctx context.Context, req *authnv1.OnlineSessionRequest) (*emptypb.Empty, error) {
	principal, ok := authnservice.PrincipalFromContext(ctx)
	if !ok {
		err := authndomain.ErrCredentialMissing
		logx.Propagate(ctx, s.logger, "authn.delete_my_online_session", err)
		return nil, err
	}
	if err := s.svc.LogoutOwned(ctx, principal.SystemUserID(), req.GetTokenId()); err != nil {
		logx.Propagate(ctx, s.logger, "authn.delete_my_online_session", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
