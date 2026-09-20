package auditinfra

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/audit"
	auditentity "glex/internal/domain/audit/entity"
	auditrepo "glex/internal/domain/audit/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/operlog"
	"glex/internal/infra/store"
	"glex/pkg/listq"
)

// toBizOperLog maps an ent OperLog PO to an auditentity.OperLog DO.
func toBizOperLog(po *ent.OperLog) *auditentity.OperLog {
	return &auditentity.OperLog{
		ID:           po.ID,
		Operation:    po.Operation,
		HTTPMethod:   po.HTTPMethod,
		OperUserID:   po.OperUserID,
		OperUserName: po.OperUserName,
		IPAddr:       po.Ipaddr,
		Request:      po.Request,
		Status:       po.Status,
		Reason:       po.Reason,
		ErrMsg:       po.ErrorMsg,
		LatencyMS:    po.LatencyMs,
		RequestID:    po.RequestID,
		CreateBy:     po.CreateBy,
		UpdateBy:     po.UpdateBy,
		CreatedAt:    po.CreatedAt,
		UpdatedAt:    po.UpdatedAt,
	}
}

type operLogRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewOperLogRepo creates a new OperLogRepo instance.
func NewOperLogRepo(data *store.Data) auditrepo.OperLogRepo {
	return &operLogRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.audit.oper_log")),
	}
}

// auditStoreError is the log-and-map for unexpected audit-store failures
// (ADR 0007): the raw error is logged once at the handling site with its
// operation and returned as the module's sentinel, so no raw ent error
// crosses the repository boundary. Expected business conditions
// (ent.IsNotFound and friends) map silently at the call sites; a canceled
// request is benign and returns the sentinel without logging.
func auditStoreError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return audit.ErrAuditStoreUnavailable
	}
	logger.ErrorContext(ctx, "audit store operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return audit.ErrAuditStoreUnavailable
}

func (r *operLogRepo) FindByID(ctx context.Context, id int64) (*auditentity.OperLog, error) {
	po, err := r.data.DB.OperLog(ctx).Query().
		Where(operlog.IDEQ(id), operlog.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, audit.ErrOperLogNotFound
		}
		return nil, auditStoreError(ctx, r.logger, "audit.oper_log.find_by_id", err)
	}
	return toBizOperLog(po), nil
}

func (r *operLogRepo) List(ctx context.Context, q listq.Query) ([]*auditentity.OperLog, error) {
	pos, err := r.data.DB.OperLog(ctx).Query().
		Where(
			operlog.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, auditStoreError(ctx, r.logger, "audit.oper_log.list", err)
	}
	logs := make([]*auditentity.OperLog, 0, len(pos))
	for _, po := range pos {
		logs = append(logs, toBizOperLog(po))
	}
	return logs, nil
}

// Create inserts the record. The middleware records outside any business
// transaction (ADR 0012): a single-row insert is its own atomic story.
func (r *operLogRepo) Create(ctx context.Context, o *auditentity.OperLog) (*auditentity.OperLog, error) {
	po, err := r.data.DB.OperLog(ctx).Create().
		SetID(newSnowflakeID()).
		SetOperation(o.Operation).
		SetHTTPMethod(o.HTTPMethod).
		SetOperUserID(o.OperUserID).
		SetOperUserName(o.OperUserName).
		SetIpaddr(o.IPAddr).
		SetRequest(o.Request).
		SetStatus(o.Status).
		SetReason(o.Reason).
		SetErrorMsg(o.ErrMsg).
		SetLatencyMs(o.LatencyMS).
		SetRequestID(o.RequestID).
		SetCreateBy(o.CreateBy).
		SetUpdateBy(o.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, auditStoreError(ctx, r.logger, "audit.oper_log.create", err)
	}
	return toBizOperLog(po), nil
}

// Delete withdraws the record (ADR 0002): the row stays as audit history of
// the withdrawal itself.
func (r *operLogRepo) Delete(ctx context.Context, id, actorID int64) error {
	err := r.data.DB.OperLog(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return audit.ErrOperLogNotFound
		}
		return auditStoreError(ctx, r.logger, "audit.oper_log.delete", err)
	}
	return nil
}
