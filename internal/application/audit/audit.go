package audit

import (
	"context"
	"log/slog"

	auditv1 "glex/api/audit/v1"
	"glex/internal/bizpkg"
	auditentity "glex/internal/domain/audit/entity"
	auditservice "glex/internal/domain/audit/service"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"go.einride.tech/aip/pagination"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertOperLog maps an auditentity.OperLog DO to the proto OperLog DTO.
// create_by/update_by are the row's own mechanics and never wire-visible:
// the operator fields (oper_user_id/oper_user_name) are the record's content.
func convertOperLog(o *auditentity.OperLog) *auditv1.OperLog {
	return &auditv1.OperLog{
		OperLogId:    o.ID,
		Operation:    o.Operation,
		HttpMethod:   o.HTTPMethod,
		OperUserId:   o.OperUserID,
		OperUserName: o.OperUserName,
		Ipaddr:       o.IPAddr,
		Request:      o.Request,
		Status:       operStatusToProto(o.Status),
		Reason:       o.Reason,
		ErrorMsg:     o.ErrMsg,
		LatencyMs:    o.LatencyMS,
		RequestId:    o.RequestID,
		CreatedAt:    timestamppb.New(o.CreatedAt),
	}
}

// convertLoginLog maps an auditentity.LoginLog DO to the proto LoginLog DTO.
func convertLoginLog(l *auditentity.LoginLog) *auditv1.LoginLog {
	return &auditv1.LoginLog{
		LoginLogId: l.ID,
		UserId:     l.UserID,
		UserName:   l.UserName,
		Ipaddr:     l.IPAddr,
		Browser:    l.Browser,
		Os:         l.OS,
		Status:     loginStatusToProto(l.Status),
		Msg:        l.Msg,
		CreatedAt:  timestamppb.New(l.CreatedAt),
	}
}

// Status enumerations map stored 0/1 (RuoYi status) to the wire enums
// exactly once per direction; unknown values normalize to UNSPECIFIED.
func operStatusToProto(status int8) auditv1.OperStatus {
	if status == 1 {
		return auditv1.OperStatus_OPER_STATUS_FAILURE
	}
	return auditv1.OperStatus_OPER_STATUS_SUCCESS
}

func loginStatusToProto(status int8) auditv1.LoginStatus {
	if status == 1 {
		return auditv1.LoginStatus_LOGIN_STATUS_FAILURE
	}
	return auditv1.LoginStatus_LOGIN_STATUS_SUCCESS
}

// AuditApplication implements the monitor log services: the read/withdraw
// view over Oper Logs and Login Logs. It has no Transaction — rows are
// single-row writes minted by their capture sites outside any business
// transaction (ADR 0012), so the domain service joins no ambient one here
// either. Authentication and the operations' Functional Permission policy
// are enforced by the registered HTTP middleware.
type AuditApplication struct {
	auditv1.UnimplementedOperLogServiceServer
	auditv1.UnimplementedLoginLogServiceServer

	svc    auditservice.Audit
	logger *slog.Logger
}

// NewAuditApplication creates an AuditApplication. svc is the audit domain
// service — the module's only repository holder.
func NewAuditApplication(svc auditservice.Audit, logger *slog.Logger) *AuditApplication {
	return &AuditApplication{
		svc:    svc,
		logger: logger.With(slog.String("component", "application.audit")),
	}
}

// ListOperLogs lists recorded operations with AIP-160 filter, AIP-132
// order, and page-token pagination. Newest first by default — a monitor
// read.
func (s *AuditApplication) ListOperLogs(ctx context.Context, req *auditv1.ListOperLogsRequest) (*auditv1.OperLogSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("operation", filtering.TypeString),
		filtering.DeclareIdent("oper_user_name", filtering.TypeString),
		filtering.DeclareIdent("ipaddr", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_oper_logs", err)
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_oper_logs", err)
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_oper_logs", err)
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_oper_logs", err)
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		orderBy.Fields = []ordering.Field{{Path: "created_at", Desc: true}, {Path: "id", Desc: true}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	logs, err := s.svc.ListOperLogs(ctx, filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_oper_logs", err)
		return nil, err
	}
	set := &auditv1.OperLogSet{OperLogs: make([]*auditv1.OperLog, 0, len(logs))}
	for _, o := range logs {
		set.OperLogs = append(set.OperLogs, convertOperLog(o))
	}
	if len(logs) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// GetOperLog retrieves one recorded operation by id.
func (s *AuditApplication) GetOperLog(ctx context.Context, req *auditv1.GetOperLogRequest) (*auditv1.OperLog, error) {
	o, err := s.svc.GetOperLog(ctx, req.GetOperLogId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.get_oper_log", err)
		return nil, err
	}
	return convertOperLog(o), nil
}

// DeleteOperLog withdraws one recorded operation by id (ADR 0002).
func (s *AuditApplication) DeleteOperLog(ctx context.Context, req *auditv1.DeleteOperLogRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteOperLog(ctx, req.GetOperLogId(), bizpkg.AuditActor(ctx)); err != nil {
		logx.Propagate(ctx, s.logger, "audit.delete_oper_log", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListLoginLogs lists recorded sign-ins with AIP-160 filter, AIP-132
// order, and page-token pagination. Newest first by default.
func (s *AuditApplication) ListLoginLogs(ctx context.Context, req *auditv1.ListLoginLogsRequest) (*auditv1.LoginLogSet, error) {
	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("ipaddr", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_login_logs", err)
		return nil, err
	}
	filter, err := filtering.ParseFilter(req, declarations)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_login_logs", err)
		return nil, err
	}
	pageToken, err := pagination.ParsePageToken(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_login_logs", err)
		return nil, err
	}
	orderBy, err := ordering.ParseOrderBy(req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_login_logs", err)
		return nil, err
	}
	if len(orderBy.Fields) == 0 {
		orderBy.Fields = []ordering.Field{{Path: "created_at", Desc: true}, {Path: "id", Desc: true}}
	}
	if req.GetPageSize() <= 0 {
		req.PageSize = 20
	}
	logs, err := s.svc.ListLoginLogs(ctx, filter, orderBy, int(req.GetPageSize()), int(pageToken.Offset))
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_login_logs", err)
		return nil, err
	}
	set := &auditv1.LoginLogSet{LoginLogs: make([]*auditv1.LoginLog, 0, len(logs))}
	for _, l := range logs {
		set.LoginLogs = append(set.LoginLogs, convertLoginLog(l))
	}
	if len(logs) >= int(req.GetPageSize()) {
		set.NextPageToken = pageToken.Next(req).String()
	}
	return set, nil
}

// DeleteLoginLog withdraws one recorded sign-in by id (ADR 0002).
func (s *AuditApplication) DeleteLoginLog(ctx context.Context, req *auditv1.DeleteLoginLogRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteLoginLog(ctx, req.GetLoginLogId(), bizpkg.AuditActor(ctx)); err != nil {
		logx.Propagate(ctx, s.logger, "audit.delete_login_log", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
