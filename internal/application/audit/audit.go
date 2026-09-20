package audit

import (
	"context"
	"log/slog"

	auditv1 "glex/api/audit/v1"
	"glex/internal/bizpkg"
	auditentity "glex/internal/domain/audit/entity"
	auditservice "glex/internal/domain/audit/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
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

// operLogListSpec is this module's list fact (ADR 0024).
var operLogListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("operation", filtering.TypeString),
		filtering.DeclareIdent("oper_user_name", filtering.TypeString),
		filtering.DeclareIdent("ipaddr", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
	},
	DefaultOrder: []ordering.Field{{Path: "created_at", Desc: true}, {Path: "id", Desc: true}},
}

// ListOperLogs lists recorded operations with AIP-160 filter, AIP-132
// order, and page-token pagination. Newest first by default — a monitor
// read.
func (s *AuditApplication) ListOperLogs(ctx context.Context, req *auditv1.ListOperLogsRequest) (*auditv1.OperLogSet, error) {
	q, page, err := listq.Parse(req, operLogListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_oper_logs", err)
		return nil, err
	}
	logs, err := s.svc.ListOperLogs(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_oper_logs", err)
		return nil, err
	}
	set := &auditv1.OperLogSet{OperLogs: make([]*auditv1.OperLog, 0, len(logs))}
	for _, o := range logs {
		set.OperLogs = append(set.OperLogs, convertOperLog(o))
	}
	set.NextPageToken = page.NextToken(len(logs))
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

// loginLogListSpec is this module's list fact (ADR 0024).
var loginLogListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("ipaddr", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
	},
	DefaultOrder: []ordering.Field{{Path: "created_at", Desc: true}, {Path: "id", Desc: true}},
}

// ListLoginLogs lists recorded sign-ins with AIP-160 filter, AIP-132
// order, and page-token pagination. Newest first by default.
func (s *AuditApplication) ListLoginLogs(ctx context.Context, req *auditv1.ListLoginLogsRequest) (*auditv1.LoginLogSet, error) {
	q, page, err := listq.Parse(req, loginLogListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_login_logs", err)
		return nil, err
	}
	logs, err := s.svc.ListLoginLogs(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.list_login_logs", err)
		return nil, err
	}
	set := &auditv1.LoginLogSet{LoginLogs: make([]*auditv1.LoginLog, 0, len(logs))}
	for _, l := range logs {
		set.LoginLogs = append(set.LoginLogs, convertLoginLog(l))
	}
	set.NextPageToken = page.NextToken(len(logs))
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
