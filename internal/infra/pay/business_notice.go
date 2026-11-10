package payinfra

import (
	"context"
	"log/slog"

	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/store"
)

type businessNoticeRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewBusinessNoticeRepo creates the Business Notification repo.
func NewBusinessNoticeRepo(data *store.Data) payrepo.BusinessNoticeRepo {
	return &businessNoticeRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.pay.business_notice")),
	}
}

func (r *businessNoticeRepo) storeError(ctx context.Context, operation string, err error) error {
	return payStoreError(ctx, r.logger, operation, err)
}

func (r *businessNoticeRepo) NextID() int64 {
	return newSnowflakeID()
}

// Insert records one notice. A unique violation is the one-notice rule —
// the payment or the refund already has its row — and is not an error.
func (r *businessNoticeRepo) Insert(ctx context.Context, n *payentity.BusinessNotice) (bool, error) {
	create := r.data.DB.PayBusinessNotice(ctx).Create().
		SetID(n.ID).
		SetApplicationID(n.ApplicationID).
		SetKind(int16(n.Kind)).
		SetPaymentID(n.PaymentID).
		SetBizNo(n.BizNo).
		SetNotifyURL(n.NotifyURL).
		SetStatus(int16(n.Status)).
		SetAttemptCount(n.AttemptCount).
		SetCreateBy(n.CreateBy).
		SetUpdateBy(n.UpdateBy)
	if n.RefundID != 0 {
		create.SetRefundID(n.RefundID)
	}
	if !n.NextAttemptAt.IsZero() {
		create.SetNextAttemptAt(n.NextAttemptAt)
	}
	if _, err := create.Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			return false, nil
		}
		return false, r.storeError(ctx, "pay.business_notice.insert", err)
	}
	return true, nil
}
