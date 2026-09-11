package payinfra

import (
	"context"
	"log/slog"
	"time"

	"glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	entrefund "glex/internal/infra/ent/payrefund"
	"glex/internal/infra/ent/predicate"
	"glex/internal/infra/store"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/infra/ent"
)

// toBizRefund maps an ent PayRefund PO to a payentity.Refund DO.
func toBizRefund(po *ent.PayRefund) *payentity.Refund {
	return &payentity.Refund{
		ID:              po.ID,
		PaymentID:       po.PaymentID,
		MerchantID:      po.MerchantID,
		Channel:         payentity.Channel(po.Channel),
		Amount:          po.Amount,
		Reason:          po.Reason,
		Status:          payentity.RefundStatus(po.Status),
		ChannelRefundNo: po.ChannelRefundNo,
		FailReason:      po.FailReason,
		RefundedAt:      po.RefundedAt,
		CreateBy:        po.CreateBy,
		UpdateBy:        po.UpdateBy,
		CreatedAt:       po.CreatedAt,
		UpdatedAt:       po.UpdatedAt,
	}
}

type refundRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewRefundRepo creates the Refund repo.
func NewRefundRepo(data *store.Data) payrepo.RefundRepo {
	return &refundRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.pay.refund")),
	}
}

// storeError is this repo's log-and-map (ADR 0007).
func (r *refundRepo) storeError(ctx context.Context, operation string, err error) error {
	return payStoreError(ctx, r.logger, operation, err)
}

// PendingRefundSum sums the payment's still-Pending refund amounts in one
// aggregate — the in-flight reservation the cap check counts.
func (r *refundRepo) PendingRefundSum(ctx context.Context, paymentID int64) (int64, error) {
	var sums []int64
	err := r.data.DB.PayRefund(ctx).Query().
		Where(
			entrefund.PaymentIDEQ(paymentID),
			entrefund.StatusEQ(int16(payentity.RefundPending)),
			entrefund.DeletedAtIsNil(),
		).
		Aggregate(ent.As(ent.Sum(entrefund.FieldAmount), "sum")).
		Scan(ctx, &sums)
	if err != nil {
		return 0, r.storeError(ctx, "pay.refund.pending_sum", err)
	}
	if len(sums) == 0 {
		return 0, nil
	}
	return sums[0], nil
}

func (r *refundRepo) NextID() int64 {
	return newSnowflakeID()
}

func (r *refundRepo) FindByID(ctx context.Context, id int64) (*payentity.Refund, error) {
	po, err := r.data.DB.PayRefund(ctx).Query().
		Where(entrefund.IDEQ(id), entrefund.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrRefundNotFound
		}
		return nil, r.storeError(ctx, "pay.refund.find_by_id", err)
	}
	return toBizRefund(po), nil
}

func (r *refundRepo) List(ctx context.Context, opts ...payrepo.ListOption) ([]*payentity.Refund, error) {
	o := payrepo.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.PayRefund(ctx).Query().
		Where(
			entrefund.DeletedAtIsNil(),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "pay.refund.list", err)
	}
	refunds := make([]*payentity.Refund, 0, len(pos))
	for _, po := range pos {
		refunds = append(refunds, toBizRefund(po))
	}
	return refunds, nil
}

func (r *refundRepo) Create(ctx context.Context, rf *payentity.Refund) (*payentity.Refund, error) {
	po, err := r.data.DB.PayRefund(ctx).Create().
		SetID(rf.ID).
		SetPaymentID(rf.PaymentID).
		SetMerchantID(rf.MerchantID).
		SetChannel(int16(rf.Channel)).
		SetAmount(rf.Amount).
		SetReason(rf.Reason).
		SetStatus(int16(rf.Status)).
		SetCreateBy(rf.CreateBy).
		SetUpdateBy(rf.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "pay.refund.create", err)
	}
	return toBizRefund(po), nil
}

// pendingRefundScope is the CAS guard shared by both transitions: the row
// must still be alive and Pending. Zero affected rows is the by-design
// no-op (duplicate or out-of-order receipt), never an error.
func pendingRefundScope(id int64) []predicate.PayRefund {
	return []predicate.PayRefund{
		entrefund.IDEQ(id),
		entrefund.StatusEQ(int16(payentity.RefundPending)),
		entrefund.DeletedAtIsNil(),
	}
}

func (r *refundRepo) MarkSuccess(ctx context.Context, id int64, channelRefundNo string, refundedAt time.Time) (bool, error) {
	if refundedAt.IsZero() {
		refundedAt = time.Now()
	}
	upd := r.data.DB.PayRefund(ctx).Update().
		Where(pendingRefundScope(id)...).
		SetStatus(int16(payentity.RefundSuccess)).
		SetRefundedAt(refundedAt).
		SetUpdateBy(systemActor)
	if channelRefundNo != "" {
		upd.SetChannelRefundNo(channelRefundNo)
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return false, r.storeError(ctx, "pay.refund.mark_success", err)
	}
	return n > 0, nil
}

func (r *refundRepo) MarkFailed(ctx context.Context, id int64, reason string) (bool, error) {
	upd := r.data.DB.PayRefund(ctx).Update().
		Where(pendingRefundScope(id)...).
		SetStatus(int16(payentity.RefundFailed)).
		SetUpdateBy(systemActor)
	if reason != "" {
		upd.SetFailReason(truncateReason(reason))
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return false, r.storeError(ctx, "pay.refund.mark_failed", err)
	}
	return n > 0, nil
}
