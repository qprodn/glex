package service

import (
	"context"
	"log/slog"

	"glex/internal/domain/audit/entity"
	"glex/internal/domain/audit/repository"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// Audit is the audit domain service behind the monitor log endpoints: the
// only path to the Oper Log and Login Log reads, writes, and withdrawals
// (ADR 0005). Recording methods are called by the capture sites — the
// transport middleware for Oper Logs, authn for Login Logs (ADR 0012);
// query methods serve the monitor pages. Methods join the caller's ambient
// transaction; recorders run outside any business transaction, best-effort
// (ADR 0012). Every error a method returns to its caller first logs its
// Warn breadcrumb (ADR 0011).
type Audit interface {
	// CreateOperLog validates and persists one operation record.
	CreateOperLog(ctx context.Context, o *entity.OperLog) (*entity.OperLog, error)
	GetOperLog(ctx context.Context, id int64) (*entity.OperLog, error)
	ListOperLogs(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.OperLog, error)
	// DeleteOperLog withdraws one operation record (ADR 0002).
	DeleteOperLog(ctx context.Context, id, actorID int64) error

	// CreateLoginLog validates and persists one sign-in/session-end record.
	CreateLoginLog(ctx context.Context, l *entity.LoginLog) (*entity.LoginLog, error)
	ListLoginLogs(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.LoginLog, error)
	// DeleteLoginLog withdraws one sign-in record (ADR 0002).
	DeleteLoginLog(ctx context.Context, id, actorID int64) error
}

type auditImpl struct {
	operLogs  repository.OperLogRepo
	loginLogs repository.LoginLogRepo
	logger    *slog.Logger
}

// NewAuditService creates the audit domain service. operLogs backs the Oper
// Log record/query/withdraw paths; loginLogs the Login Log ones.
func NewAuditService(operLogs repository.OperLogRepo, loginLogs repository.LoginLogRepo, logger *slog.Logger) Audit {
	return &auditImpl{
		operLogs:  operLogs,
		loginLogs: loginLogs,
		logger:    logger.With(slog.String("component", "domain.audit.svc")),
	}
}

// --- Oper Log (操作日志) ---

func (s *auditImpl) CreateOperLog(ctx context.Context, o *entity.OperLog) (*entity.OperLog, error) {
	if err := o.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.create_oper_log", err)
		return nil, err
	}
	created, err := s.operLogs.Create(ctx, o)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.create_oper_log", err)
		return nil, err
	}
	return created, nil
}

func (s *auditImpl) GetOperLog(ctx context.Context, id int64) (*entity.OperLog, error) {
	o, err := s.operLogs.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.get_oper_log", err)
		return nil, err
	}
	return o, nil
}

func (s *auditImpl) ListOperLogs(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.OperLog, error) {
	os, err := s.operLogs.List(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
		repository.ListLimit(limit),
		repository.ListOffset(offset),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.list_oper_logs", err)
		return nil, err
	}
	return os, nil
}

func (s *auditImpl) DeleteOperLog(ctx context.Context, id, actorID int64) error {
	if err := s.operLogs.Delete(ctx, id, actorID); err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.delete_oper_log", err)
		return err
	}
	return nil
}

// --- Login Log (登录日志) ---

func (s *auditImpl) CreateLoginLog(ctx context.Context, l *entity.LoginLog) (*entity.LoginLog, error) {
	if err := l.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.create_login_log", err)
		return nil, err
	}
	created, err := s.loginLogs.Create(ctx, l)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.create_login_log", err)
		return nil, err
	}
	return created, nil
}

func (s *auditImpl) ListLoginLogs(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.LoginLog, error) {
	ls, err := s.loginLogs.List(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
		repository.ListLimit(limit),
		repository.ListOffset(offset),
	)
	if err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.list_login_logs", err)
		return nil, err
	}
	return ls, nil
}

func (s *auditImpl) DeleteLoginLog(ctx context.Context, id, actorID int64) error {
	if err := s.loginLogs.Delete(ctx, id, actorID); err != nil {
		logx.Propagate(ctx, s.logger, "audit.svc.delete_login_log", err)
		return err
	}
	return nil
}
