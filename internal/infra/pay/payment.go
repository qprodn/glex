package payinfra

import (
	"context"
	"log/slog"
	"time"

	"glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/ent"
	entpayment "glex/internal/infra/ent/paypayment"
	"glex/internal/infra/ent/predicate"
	"glex/internal/infra/store"

	"github.com/go-kratos/aip-go/ents"
)

// toBizPayment maps an ent PayPayment PO to a payentity.Payment DO.
func toBizPayment(po *ent.PayPayment) *payentity.Payment {
	return &payentity.Payment{
		ID:             po.ID,
		MerchantID:     po.MerchantID,
		Channel:        payentity.Channel(po.Channel),
		BizNo:          po.BizNo,
		Subject:        po.Subject,
		Amount:         po.Amount,
		Currency:       po.Currency,
		PayMethod:      payentity.PayMethod(po.PayMethod),
		Status:         payentity.PaymentStatus(po.Status),
		ChannelTradeNo: po.ChannelTradeNo,
		ChannelPayload: po.ChannelPayload,
		PayerID:        po.PayerID,
		RefundedAmount: po.RefundedAmount,
		FailReason:     po.FailReason,
		ExpireTime:     po.ExpireTime,
		PaidAt:         po.PaidAt,
		CreateBy:       po.CreateBy,
		UpdateBy:       po.UpdateBy,
		CreatedAt:      po.CreatedAt,
		UpdatedAt:      po.UpdatedAt,
	}
}

type paymentRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewPaymentRepo creates the Payment repo.
func NewPaymentRepo(data *store.Data) payrepo.PaymentRepo {
	return &paymentRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.pay.payment")),
	}
}

// storeError is this repo's log-and-map (ADR 0007).
func (r *paymentRepo) storeError(ctx context.Context, operation string, err error) error {
	return payStoreError(ctx, r.logger, operation, err)
}

func (r *paymentRepo) NextID() int64 {
	return newSnowflakeID()
}

func (r *paymentRepo) FindByID(ctx context.Context, id int64) (*payentity.Payment, error) {
	po, err := r.data.DB.PayPayment(ctx).Query().
		Where(entpayment.IDEQ(id), entpayment.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrPaymentNotFound
		}
		return nil, r.storeError(ctx, "pay.payment.find_by_id", err)
	}
	return toBizPayment(po), nil
}

// FindLatestByBizNo resolves the newest active Payment for one merchant's
// Business Order Number — re-initiation keeps history, and the live row
// is the newest one.
func (r *paymentRepo) FindLatestByBizNo(ctx context.Context, merchantID int64, bizNo string) (*payentity.Payment, error) {
	po, err := r.data.DB.PayPayment(ctx).Query().
		Where(
			entpayment.MerchantIDEQ(merchantID),
			entpayment.BizNoEQ(bizNo),
			entpayment.DeletedAtIsNil(),
		).
		Order(ent.Desc(entpayment.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrPaymentNotFound
		}
		return nil, r.storeError(ctx, "pay.payment.find_latest_by_biz_no", err)
	}
	return toBizPayment(po), nil
}

func (r *paymentRepo) List(ctx context.Context, opts ...payrepo.ListOption) ([]*payentity.Payment, error) {
	o := payrepo.ListOptions{Limit: 20}
	for _, opt := range opts {
		opt(&o)
	}
	pos, err := r.data.DB.PayPayment(ctx).Query().
		Where(
			entpayment.DeletedAtIsNil(),
			ents.ApplyFilter(o.Filter),
		).
		Order(ents.ApplyOrderBy(o.OrderBy)).
		Offset(o.Offset).
		Limit(o.Limit).
		All(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "pay.payment.list", err)
	}
	payments := make([]*payentity.Payment, 0, len(pos))
	for _, po := range pos {
		payments = append(payments, toBizPayment(po))
	}
	return payments, nil
}

func (r *paymentRepo) Create(ctx context.Context, p *payentity.Payment) (*payentity.Payment, error) {
	po, err := r.data.DB.PayPayment(ctx).Create().
		SetID(p.ID).
		SetMerchantID(p.MerchantID).
		SetChannel(int16(p.Channel)).
		SetBizNo(p.BizNo).
		SetSubject(p.Subject).
		SetAmount(p.Amount).
		SetCurrency(p.Currency).
		SetPayMethod(int16(p.PayMethod)).
		SetStatus(int16(p.Status)).
		SetExpireTime(p.ExpireTime).
		SetCreateBy(p.CreateBy).
		SetUpdateBy(p.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "pay.payment.create", err)
	}
	return toBizPayment(po), nil
}

// ClosePendingByBizNo closes every still-Pending Payment of one merchant's
// Business Order Number in one statement — the re-initiation rule's bulk
// half. The count is informational; a concurrent notification losing the
// race is a CAS no-op, not an error.
func (r *paymentRepo) ClosePendingByBizNo(ctx context.Context, merchantID int64, bizNo string, actorID int64) (int, error) {
	n, err := r.data.DB.PayPayment(ctx).Update().
		Where(
			entpayment.MerchantIDEQ(merchantID),
			entpayment.BizNoEQ(bizNo),
			entpayment.StatusEQ(int16(payentity.PaymentPending)),
			entpayment.DeletedAtIsNil(),
		).
		SetStatus(int16(payentity.PaymentClosed)).
		SetUpdateBy(actorID).
		Save(ctx)
	if err != nil {
		return 0, r.storeError(ctx, "pay.payment.close_pending_by_biz_no", err)
	}
	return n, nil
}

func (r *paymentRepo) SetChannelPayload(ctx context.Context, id int64, payload string) error {
	err := r.data.DB.PayPayment(ctx).UpdateOneID(id).
		SetChannelPayload(payload).
		Exec(ctx)
	if ent.IsNotFound(err) {
		return pay.ErrPaymentNotFound
	}
	if err != nil {
		return r.storeError(ctx, "pay.payment.set_channel_payload", err)
	}
	return nil
}

func (r *paymentRepo) SetFailReason(ctx context.Context, id int64, reason string) error {
	err := r.data.DB.PayPayment(ctx).UpdateOneID(id).
		SetFailReason(reason).
		Exec(ctx)
	if ent.IsNotFound(err) {
		return pay.ErrPaymentNotFound
	}
	if err != nil {
		return r.storeError(ctx, "pay.payment.set_fail_reason", err)
	}
	return nil
}

// pendingPaymentScope is the CAS guard shared by every transition: the row
// must still be alive and Pending. A zero affected-row count is the
// by-design no-op (duplicate or out-of-order receipt), never an error.
func pendingPaymentScope(id int64) []predicate.PayPayment {
	return []predicate.PayPayment{
		entpayment.IDEQ(id),
		entpayment.StatusEQ(int16(payentity.PaymentPending)),
		entpayment.DeletedAtIsNil(),
	}
}

func (r *paymentRepo) MarkPaid(ctx context.Context, id int64, capture payentity.PaidCapture) (bool, error) {
	paidAt := capture.PaidAt
	if paidAt.IsZero() {
		paidAt = time.Now()
	}
	upd := r.data.DB.PayPayment(ctx).Update().
		Where(pendingPaymentScope(id)...).
		SetStatus(int16(payentity.PaymentPaid)).
		SetPaidAt(paidAt).
		SetUpdateBy(systemActor)
	if capture.ChannelTradeNo != "" {
		upd.SetChannelTradeNo(capture.ChannelTradeNo)
	}
	if capture.PayerID != "" {
		upd.SetPayerID(capture.PayerID)
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return false, r.storeError(ctx, "pay.payment.mark_paid", err)
	}
	return n > 0, nil
}

// BackfillPaidInfo completes payer evidence on an already-Paid row whose
// earlier confirmation lacked it — the one allowed write to a Paid
// payment besides the refund sum (design §5). Never overwrites a value.
func (r *paymentRepo) BackfillPaidInfo(ctx context.Context, id int64, capture payentity.PaidCapture) (bool, error) {
	preds := []predicate.PayPayment{
		entpayment.IDEQ(id),
		entpayment.StatusEQ(int16(payentity.PaymentPaid)),
		entpayment.DeletedAtIsNil(),
	}
	// Only the still-empty fields are backfilled; existing evidence stays.
	if capture.ChannelTradeNo != "" {
		preds = append(preds, entpayment.ChannelTradeNoIsNil())
	}
	if capture.PayerID != "" {
		preds = append(preds, entpayment.PayerIDIsNil())
	}
	if !capture.PaidAt.IsZero() {
		preds = append(preds, entpayment.PaidAtIsNil())
	}
	upd := r.data.DB.PayPayment(ctx).Update().Where(preds...)
	if capture.ChannelTradeNo != "" {
		upd.SetChannelTradeNo(capture.ChannelTradeNo)
	}
	if capture.PayerID != "" {
		upd.SetPayerID(capture.PayerID)
	}
	if !capture.PaidAt.IsZero() {
		upd.SetPaidAt(capture.PaidAt)
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return false, r.storeError(ctx, "pay.payment.backfill_paid_info", err)
	}
	return n > 0, nil
}

func (r *paymentRepo) MarkFailed(ctx context.Context, id int64, reason string) (bool, error) {
	upd := r.data.DB.PayPayment(ctx).Update().
		Where(pendingPaymentScope(id)...).
		SetStatus(int16(payentity.PaymentFailed)).
		SetUpdateBy(systemActor)
	if reason != "" {
		upd.SetFailReason(truncateReason(reason))
	}
	n, err := upd.Save(ctx)
	if err != nil {
		return false, r.storeError(ctx, "pay.payment.mark_failed", err)
	}
	return n > 0, nil
}

func (r *paymentRepo) MarkClosed(ctx context.Context, id int64) (bool, error) {
	n, err := r.data.DB.PayPayment(ctx).Update().
		Where(pendingPaymentScope(id)...).
		SetStatus(int16(payentity.PaymentClosed)).
		SetUpdateBy(systemActor).
		Save(ctx)
	if err != nil {
		return false, r.storeError(ctx, "pay.payment.mark_closed", err)
	}
	return n > 0, nil
}

func (r *paymentRepo) MarkExpired(ctx context.Context, id int64) (bool, error) {
	n, err := r.data.DB.PayPayment(ctx).Update().
		Where(pendingPaymentScope(id)...).
		SetStatus(int16(payentity.PaymentExpired)).
		SetUpdateBy(systemActor).
		Save(ctx)
	if err != nil {
		return false, r.storeError(ctx, "pay.payment.mark_expired", err)
	}
	return n > 0, nil
}

// systemActor marks channel-driven transitions as the system itself
// acting (ADR 0002 — 0 is the system).
const systemActor = int64(0)

// truncateReason bounds a channel-supplied reason to the column's length,
// rune-safe (channel texts are frequently Chinese).
func truncateReason(reason string) string {
	return truncateUTF8(reason, 256)
}
