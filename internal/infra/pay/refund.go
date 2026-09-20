package payinfra

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	entpayment "glex/internal/infra/ent/paypayment"
	entrefund "glex/internal/infra/ent/payrefund"
	"glex/internal/infra/ent/predicate"
	"glex/internal/infra/store"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/infra/ent"
	"glex/pkg/listq"
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
// aggregate — the in-flight reservation the cap check counts. SUM over zero
// rows returns one row carrying NULL on Postgres, so the scan target must
// be null-safe.
func (r *refundRepo) PendingRefundSum(ctx context.Context, paymentID int64) (int64, error) {
	var sums []struct {
		Sum sql.NullInt64 `sql:"sum"`
	}
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
	if len(sums) == 0 || !sums[0].Sum.Valid {
		return 0, nil
	}
	return sums[0].Sum.Int64, nil
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

func (r *refundRepo) List(ctx context.Context, q listq.Query) ([]*payentity.Refund, error) {
	pos, err := r.data.DB.PayRefund(ctx).Query().
		Where(
			entrefund.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
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

// MarkSuccess settles the refund and advances the payment's refunded sum
// in one transaction: an understated sum (refund SUCCESS, accumulate lost)
// would let the cap check admit more than was paid, so the pair moves
// together or not at all. A lost CAS (duplicate delivery) skips the
// accumulate — the sum must move exactly once with the status.
func (r *refundRepo) MarkSuccess(ctx context.Context, id, paymentID, amount int64, channelRefundNo string, refundedAt time.Time) (bool, error) {
	if refundedAt.IsZero() {
		refundedAt = time.Now()
	}
	var advanced bool
	txErr := r.data.DB.InTx(ctx, func(ctx context.Context) error {
		upd := r.data.DB.PayRefund(ctx).Update().
			Where(pendingRefundScope(id)...).
			SetStatus(int16(payentity.RefundSuccess)).
			SetRefundedAt(refundedAt).
			SetUpdateBy(systemActor)
		if channelRefundNo != "" {
			upd.SetChannelRefundNo(channelRefundNo)
		}
		n, err := upd.Save(ctx)
		if err != nil || n == 0 {
			return err // CAS lost — a duplicate; the sum must not move
		}
		advanced = true
		moved, err := r.data.DB.PayPayment(ctx).Update().
			Where(
				entpayment.IDEQ(paymentID),
				entpayment.StatusEQ(int16(payentity.PaymentPaid)),
				entpayment.DeletedAtIsNil(),
			).
			AddRefundedAmount(amount).
			Save(ctx)
		if err != nil {
			return err
		}
		if moved == 0 {
			// The refund settled but its payment is not Paid — a ledger
			// anomaly worth surfacing even though the tx commits.
			r.logger.WarnContext(ctx, "refund settled on a payment that is not Paid; refunded sum not moved",
				slog.Int64("refund_id", id),
				slog.Int64("payment_id", paymentID),
				slog.Int64("amount", amount))
		}
		return nil
	})
	if txErr != nil {
		return false, r.storeError(ctx, "pay.refund.mark_success", txErr)
	}
	return advanced, nil
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
