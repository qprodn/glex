package payinfra

import (
	"context"
	"log/slog"

	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/ent"
	entnotice "glex/internal/infra/ent/paybusinessnotice"
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

// ListDue returns waiting notices whose next attempt time has arrived.
func (r *businessNoticeRepo) ListDue(ctx context.Context, req payrepo.ListDueNoticesRequest) ([]*payentity.BusinessNotice, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.data.DB.PayBusinessNotice(ctx).Query().
		Where(
			entnotice.DeletedAtIsNil(),
			entnotice.StatusEQ(int16(payentity.NoticeWaiting)),
			entnotice.NextAttemptAtNotNil(),
			entnotice.NextAttemptAtLTE(req.Now),
		).
		Order(ent.Asc(entnotice.FieldNextAttemptAt), ent.Asc(entnotice.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "pay.business_notice.list_due", err)
	}
	out := make([]*payentity.BusinessNotice, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizNotice(row))
	}
	return out, nil
}

// ClaimDueNotice leases a waiting, due notice so a second scan skips it.
func (r *businessNoticeRepo) ClaimDueNotice(ctx context.Context, req payrepo.ClaimDueNoticeRequest) (bool, error) {
	n, err := r.data.DB.PayBusinessNotice(ctx).Update().
		Where(
			entnotice.IDEQ(req.ID),
			entnotice.DeletedAtIsNil(),
			entnotice.StatusEQ(int16(payentity.NoticeWaiting)),
			entnotice.NextAttemptAtNotNil(),
			entnotice.NextAttemptAtLTE(req.Now),
		).
		SetNextAttemptAt(req.Until).
		SetUpdateBy(systemActor).
		Save(ctx)
	if err != nil {
		return false, r.storeError(ctx, "pay.business_notice.claim_due", err)
	}
	return n == 1, nil
}

// RecordAutomaticAttempt stores the attempt and the notice progress in one
// transaction. A lost claim (the notice is no longer waiting) records nothing.
func (r *businessNoticeRepo) RecordAutomaticAttempt(ctx context.Context, req payrepo.RecordAutomaticAttemptRequest) error {
	txErr := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		upd := r.data.DB.PayBusinessNotice(ctx).Update().
			Where(
				entnotice.IDEQ(req.Notice.ID),
				entnotice.DeletedAtIsNil(),
				entnotice.StatusEQ(int16(payentity.NoticeWaiting)),
			).
			SetStatus(int16(req.Notice.Status)).
			SetAttemptCount(req.Notice.AttemptCount).
			SetUpdateBy(systemActor)
		if req.Notice.NextAttemptAt.IsZero() {
			upd.ClearNextAttemptAt()
		} else {
			upd.SetNextAttemptAt(req.Notice.NextAttemptAt)
		}
		n, err := upd.Save(ctx)
		if err != nil || n == 0 {
			return err
		}
		create := r.data.DB.PayBusinessNoticeAttempt(ctx).Create().
			SetID(req.Attempt.ID).
			SetNoticeID(req.Attempt.NoticeID).
			SetManual(req.Attempt.Manual).
			SetError(req.Attempt.Error).
			SetAccepted(req.Attempt.Accepted).
			SetCreateBy(systemActor).
			SetUpdateBy(systemActor)
		if req.Attempt.HTTPStatus != nil {
			create.SetHTTPStatus(*req.Attempt.HTTPStatus)
		}
		if !req.Attempt.CreatedAt.IsZero() {
			create.SetCreatedAt(req.Attempt.CreatedAt)
		}
		return create.Exec(ctx)
	})
	if txErr != nil {
		return r.storeError(ctx, "pay.business_notice.record_attempt", txErr)
	}
	return nil
}

func toBizNotice(po *ent.PayBusinessNotice) *payentity.BusinessNotice {
	n := &payentity.BusinessNotice{
		ID:            po.ID,
		ApplicationID: po.ApplicationID,
		Kind:          payentity.NoticeKind(po.Kind),
		PaymentID:     po.PaymentID,
		BizNo:         po.BizNo,
		NotifyURL:     po.NotifyURL,
		Status:        payentity.NoticeStatus(po.Status),
		AttemptCount:  po.AttemptCount,
		CreateBy:      po.CreateBy,
		UpdateBy:      po.UpdateBy,
		CreatedAt:     po.CreatedAt,
		UpdatedAt:     po.UpdatedAt,
	}
	if po.RefundID != nil {
		n.RefundID = *po.RefundID
	}
	if po.NextAttemptAt != nil {
		n.NextAttemptAt = *po.NextAttemptAt
	}
	return n
}
