package payinfra

import (
	"context"
	"log/slog"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/ent"
	entapp "glex/internal/infra/ent/payapplication"
	"glex/internal/infra/store"
	"glex/pkg/listq"

	"github.com/go-kratos/aip-go/ents"
)

// toBizPayApplication maps an ent PayApplication PO to the domain object.
func toBizPayApplication(po *ent.PayApplication) *payentity.PayApplication {
	return &payentity.PayApplication{
		ID:              po.ID,
		Name:            po.Name,
		MerchantID:      po.MerchantID,
		Status:          payentity.ApplicationStatus(po.Status),
		OrderNotifyURL:  po.OrderNotifyURL,
		RefundNotifyURL: po.RefundNotifyURL,
		CreateBy:        po.CreateBy,
		UpdateBy:        po.UpdateBy,
		CreatedAt:       po.CreatedAt,
		UpdatedAt:       po.UpdatedAt,
	}
}

type payApplicationRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewPayApplicationRepo creates the Pay Application repo.
func NewPayApplicationRepo(data *store.Data) payrepo.PayApplicationRepo {
	return &payApplicationRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.pay.application")),
	}
}

func (r *payApplicationRepo) storeError(ctx context.Context, operation string, err error) error {
	return payStoreError(ctx, r.logger, operation, err)
}

func (r *payApplicationRepo) NextID() int64 {
	return newSnowflakeID()
}

func (r *payApplicationRepo) FindByIDAny(ctx context.Context, id int64) (*payentity.PayApplication, error) {
	po, err := r.data.DB.PayApplication(ctx).Query().
		Where(entapp.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrApplicationNotFound
		}
		return nil, r.storeError(ctx, "pay.application.find_by_id_any", err)
	}
	return toBizPayApplication(po), nil
}

func (r *payApplicationRepo) FindByID(ctx context.Context, id int64) (*payentity.PayApplication, error) {
	po, err := r.data.DB.PayApplication(ctx).Query().
		Where(entapp.IDEQ(id), entapp.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrApplicationNotFound
		}
		return nil, r.storeError(ctx, "pay.application.find_by_id", err)
	}
	return toBizPayApplication(po), nil
}

func (r *payApplicationRepo) List(ctx context.Context, q listq.Query) ([]*payentity.PayApplication, error) {
	pos, err := r.data.DB.PayApplication(ctx).Query().
		Where(
			entapp.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "pay.application.list", err)
	}
	apps := make([]*payentity.PayApplication, 0, len(pos))
	for _, po := range pos {
		apps = append(apps, toBizPayApplication(po))
	}
	return apps, nil
}

func (r *payApplicationRepo) Create(ctx context.Context, app *payentity.PayApplication) (*payentity.PayApplication, error) {
	po, err := r.data.DB.PayApplication(ctx).Create().
		SetID(app.ID).
		SetName(app.Name).
		SetMerchantID(app.MerchantID).
		SetStatus(int16(app.Status)).
		SetOrderNotifyURL(app.OrderNotifyURL).
		SetRefundNotifyURL(app.RefundNotifyURL).
		SetCreateBy(app.CreateBy).
		SetUpdateBy(app.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, pay.ErrApplicationDuplicate
		}
		return nil, r.storeError(ctx, "pay.application.create", err)
	}
	return toBizPayApplication(po), nil
}

func (r *payApplicationRepo) Update(ctx context.Context, req payrepo.UpdatePayApplicationRequest) (*payentity.PayApplication, error) {
	app := req.App
	n, err := r.data.DB.PayApplication(ctx).Update().
		Where(entapp.IDEQ(app.ID), entapp.DeletedAtIsNil()).
		SetName(app.Name).
		SetOrderNotifyURL(app.OrderNotifyURL).
		SetRefundNotifyURL(app.RefundNotifyURL).
		SetUpdateBy(req.ActorID).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, pay.ErrApplicationDuplicate
		}
		return nil, r.storeError(ctx, "pay.application.update", err)
	}
	if n == 0 {
		return nil, pay.ErrApplicationNotFound
	}
	return r.FindByID(ctx, app.ID)
}

func (r *payApplicationRepo) UpdateStatus(ctx context.Context, req payrepo.UpdatePayApplicationStatusRequest) error {
	n, err := r.data.DB.PayApplication(ctx).Update().
		Where(entapp.IDEQ(req.ID), entapp.DeletedAtIsNil()).
		SetStatus(int16(req.Status)).
		SetUpdateBy(req.ActorID).
		Save(ctx)
	if err != nil {
		return r.storeError(ctx, "pay.application.update_status", err)
	}
	if n == 0 {
		return pay.ErrApplicationNotFound
	}
	return nil
}

func (r *payApplicationRepo) Delete(ctx context.Context, req payrepo.DeletePayApplicationRequest) error {
	n, err := r.data.DB.PayApplication(ctx).Update().
		Where(entapp.IDEQ(req.ID), entapp.DeletedAtIsNil()).
		SetDeletedAt(deletedAt()).
		SetUpdateBy(req.ActorID).
		Save(ctx)
	if err != nil {
		return r.storeError(ctx, "pay.application.delete", err)
	}
	if n == 0 {
		return pay.ErrApplicationNotFound
	}
	return nil
}
