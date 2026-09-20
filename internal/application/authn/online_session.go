package authn

import (
	"context"
	"log/slog"

	authnv1 "glex/api/authn/v1"
	"glex/internal/bizpkg"
	auditcontract "glex/internal/crossdomain/audit"
	auditmodel "glex/internal/crossdomain/audit/model"
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
// middleware. Every completed revocation records its Login Log row,
// best-effort (ADR 0012).
type OnlineSessionApplication struct {
	authnv1.UnimplementedOnlineSessionServiceServer

	svc       authnservice.OnlineSession
	loginLogs auditcontract.LoginLogWriter
	logger    *slog.Logger
}

// NewOnlineSessionApplication creates an OnlineSessionApplication. loginLogs
// records the Login Log through the audit crossdomain contract.
func NewOnlineSessionApplication(svc authnservice.OnlineSession, loginLogs auditcontract.LoginLogWriter, logger *slog.Logger) *OnlineSessionApplication {
	return &OnlineSessionApplication{
		svc:       svc,
		loginLogs: loginLogs,
		logger:    logger.With(slog.String("component", "application.online_session")),
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
	sessions, err := s.svc.List(ctx, authnservice.ListSessionsRequest{
		Filter:  filter,
		OrderBy: orderBy,
		Limit:   int(req.GetPageSize()),
		Offset:  int(pageToken.Offset),
	})
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

// KickoutOnlineSession revokes one Session ID for an administrator. The
// kicked session is read first so the Login Log row can name its account;
// a missing session fails the kick instead of silently doing nothing.
func (s *OnlineSessionApplication) KickoutOnlineSession(ctx context.Context, req *authnv1.OnlineSessionRequest) (*emptypb.Empty, error) {
	session, err := s.svc.Get(ctx, req.GetTokenId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "authn.kickout_online_session", err)
		return nil, err
	}
	if err := s.svc.Kickout(ctx, req.GetTokenId()); err != nil {
		logx.Propagate(ctx, s.logger, "authn.kickout_online_session", err)
		return nil, err
	}
	s.recordLoginLog(ctx, &auditmodel.LoginLog{
		UserID:   session.SystemUserID,
		UserName: session.UserName,
		IPAddr:   session.IPAddress,
		Browser:  session.Browser,
		OS:       session.OS,
		Success:  true,
		Msg:      "强制退出",
		ActorID:  bizpkg.AuditActor(ctx),
	})
	return &emptypb.Empty{}, nil
}

// ListMyOnlineSessions lists only the current System User's Login Sessions.
func (s *OnlineSessionApplication) ListMyOnlineSessions(ctx context.Context, _ *emptypb.Empty) (*authnv1.OnlineSessionSet, error) {
	principal := authnservice.PrincipalFromContext(ctx)
	if principal == nil {
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
	principal := authnservice.PrincipalFromContext(ctx)
	if principal == nil {
		err := authndomain.ErrCredentialMissing
		logx.Propagate(ctx, s.logger, "authn.delete_my_online_session", err)
		return nil, err
	}
	session, _ := s.svc.Get(ctx, req.GetTokenId())
	if err := s.svc.LogoutOwned(ctx, authnservice.LogoutOwnedRequest{
		SystemUserID: principal.SystemUserID(),
		SessionID:    req.GetTokenId(),
	}); err != nil {
		logx.Propagate(ctx, s.logger, "authn.delete_my_online_session", err)
		return nil, err
	}
	// The metadata read above is only for the record; its failure (already
	// swallowed) never blocks the revocation — the principal fields fill in.
	log := &auditmodel.LoginLog{
		UserID:   principal.SystemUserID(),
		UserName: principal.UserName(),
		Success:  true,
		Msg:      "移除登录会话",
		ActorID:  principal.SystemUserID(),
	}
	if session != nil {
		log.IPAddr = session.IPAddress
		log.Browser = session.Browser
		log.OS = session.OS
	}
	s.recordLoginLog(ctx, log)
	return &emptypb.Empty{}, nil
}

// recordLoginLog writes one Login Log row through the audit crossdomain
// contract, best-effort (ADR 0012): a recording failure logs Warn at this
// handling site and never changes the operation's outcome.
func (s *OnlineSessionApplication) recordLoginLog(ctx context.Context, l *auditmodel.LoginLog) {
	if err := s.loginLogs.RecordLoginLog(ctx, l); err != nil {
		s.logger.WarnContext(ctx, "login log record failed",
			slog.String("operation", "authn.record_login_log"),
			slog.Any("error", err),
		)
	}
}
