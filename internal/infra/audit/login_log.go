package auditinfra

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/audit"
	auditentity "glex/internal/domain/audit/entity"
	auditrepo "glex/internal/domain/audit/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/loginlog"
	"glex/internal/infra/store"
)

// toBizLoginLog maps an ent LoginLog PO to an auditentity.LoginLog DO.
func toBizLoginLog(po *ent.LoginLog) *auditentity.LoginLog {
	return &auditentity.LoginLog{
		ID:        po.ID,
		UserID:    po.UserID,
		UserName:  po.UserName,
		IPAddr:    po.Ipaddr,
		Browser:   po.Browser,
		OS:        po.Os,
		Status:    po.Status,
		Msg:       po.Msg,
		CreateBy:  po.CreateBy,
		UpdateBy:  po.UpdateBy,
		CreatedAt: po.CreatedAt,
		UpdatedAt: po.UpdatedAt,
	}
}

type loginLogRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewLoginLogRepo creates a new LoginLogRepo instance.
func NewLoginLogRepo(data *store.Data) auditrepo.LoginLogRepo {
	return &loginLogRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.audit.login_log")),
	}
}

func (r *loginLogRepo) List(ctx context.Context, opts ...auditrepo.ListOption) ([]*auditentity.LoginLog, error) {
	o := auditrepo.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.LoginLog(ctx).Query().
		Where(
			loginlog.DeletedAtIsNil(),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, auditStoreError(ctx, r.logger, "audit.login_log.list", err)
	}
	logs := make([]*auditentity.LoginLog, 0, len(pos))
	for _, po := range pos {
		logs = append(logs, toBizLoginLog(po))
	}
	return logs, nil
}

// Create inserts the record. authn records outside any business transaction
// (ADR 0012): a single-row insert is its own atomic story.
func (r *loginLogRepo) Create(ctx context.Context, l *auditentity.LoginLog) (*auditentity.LoginLog, error) {
	po, err := r.data.DB.LoginLog(ctx).Create().
		SetID(newSnowflakeID()).
		SetUserID(l.UserID).
		SetUserName(l.UserName).
		SetIpaddr(l.IPAddr).
		SetBrowser(l.Browser).
		SetOs(l.OS).
		SetStatus(l.Status).
		SetMsg(l.Msg).
		SetCreateBy(l.CreateBy).
		SetUpdateBy(l.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, auditStoreError(ctx, r.logger, "audit.login_log.create", err)
	}
	return toBizLoginLog(po), nil
}

// Delete withdraws the record (ADR 0002): the row stays as audit history of
// the withdrawal itself.
func (r *loginLogRepo) Delete(ctx context.Context, id, actorID int64) error {
	err := r.data.DB.LoginLog(ctx).UpdateOneID(id).
		SetDeletedAt(time.Now()).
		SetUpdateBy(actorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return audit.ErrLoginLogNotFound
		}
		return auditStoreError(ctx, r.logger, "audit.login_log.delete", err)
	}
	return nil
}
