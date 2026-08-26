package authn

import (
	"context"
	"log/slog"

	authnv1 "glex/api/authn/v1"
	authndomain "glex/internal/domain/authn"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// OnlineSessionApplication implements the four RuoYi-compatible Login Session
// operations. Bulk revocation is intentionally absent from the service.
type OnlineSessionApplication struct {
	authnv1.UnimplementedOnlineSessionServiceServer

	uc     *LoginSessionUsecase
	logger *slog.Logger
}

// NewOnlineSessionApplication creates an OnlineSessionApplication.
func NewOnlineSessionApplication(uc *LoginSessionUsecase, logger *slog.Logger) *OnlineSessionApplication {
	return &OnlineSessionApplication{
		uc:     uc,
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
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		orderBy.Fields = []ordering.Field{{Path: "login_time", Desc: true}, {Path: "token_id"}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	sessions, err := s.uc.ListOnlineSessions(ctx,
		authndomain.ListFilter(filter),
		authndomain.ListOrderBy(orderBy),
		authndomain.ListLimit(int(req.GetPageSize())),
		authndomain.ListOffset(int(pageToken.Offset)),
	)
	if err != nil {
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
	if err := s.uc.KickoutOnlineSession(ctx, req.GetTokenId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListMyOnlineSessions lists only the current System User's Login Sessions.
func (s *OnlineSessionApplication) ListMyOnlineSessions(ctx context.Context, _ *emptypb.Empty) (*authnv1.OnlineSessionSet, error) {
	principal, ok := authndomain.PrincipalFromContext(ctx)
	if !ok {
		return nil, authndomain.ErrCredentialMissing
	}
	sessions, err := s.uc.ListSystemUserSessions(ctx, principal.SystemUserID())
	if err != nil {
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
	principal, ok := authndomain.PrincipalFromContext(ctx)
	if !ok {
		return nil, authndomain.ErrCredentialMissing
	}
	if err := s.uc.DeleteSystemUserSession(ctx, principal.SystemUserID(), req.GetTokenId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
