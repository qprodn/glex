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
	"glex/pkg/listq"

	"github.com/go-kratos/aip-go/ents"
)

// toBizPayment maps an ent PayPayment PO to a payentity.Payment DO.
func toBizPayment(po *ent.PayPayment) *payentity.Payment {
	return &payentity.Payment{
		ID:             po.ID,
		ApplicationID:  po.ApplicationID,
		MerchantID:     po.MerchantID,
		Channel:        payentity.Channel(po.Channel),
		NotifyURL:      po.NotifyURL,
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

// FindByBizNo resolves the Pay Application's Payment for one Business
// Order Number. The partial unique index guarantees at most one row.
func (r *paymentRepo) FindByBizNo(ctx context.Context, req payrepo.FindByBizNoRequest) (*payentity.Payment, error) {
	po, err := r.data.DB.PayPayment(ctx).Query().
		Where(
			entpayment.ApplicationIDEQ(req.ApplicationID),
			entpayment.BizNoEQ(req.BizNo),
			entpayment.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, pay.ErrPaymentNotFound
		}
		return nil, r.storeError(ctx, "pay.payment.find_by_biz_no", err)
	}
	return toBizPayment(po), nil
}

// ListPendingExpired scans the sweep candidates off the partial
// expire_time index (ADR 0023): still-Pending rows past their expiry, id
// ascending from afterID so a bounded batch always resumes from the oldest
// and one run can page past permanently failing rows.
func (r *paymentRepo) ListPendingExpired(ctx context.Context, req payrepo.ListPendingExpiredRequest) ([]*payentity.Payment, error) {
	pos, err := r.data.DB.PayPayment(ctx).Query().
		Where(
			entpayment.DeletedAtIsNil(),
			entpayment.StatusEQ(int16(payentity.PaymentPending)),
			entpayment.ExpireTimeLT(req.Before),
			entpayment.IDGT(req.AfterID),
		).
		Order(ent.Asc(entpayment.FieldID)).
		Limit(req.Limit).
		All(ctx)
	if err != nil {
		return nil, r.storeError(ctx, "pay.payment.list_pending_expired", err)
	}
	payments := make([]*payentity.Payment, 0, len(pos))
	for _, po := range pos {
		payments = append(payments, toBizPayment(po))
	}
	return payments, nil
}

func (r *paymentRepo) List(ctx context.Context, q listq.Query) ([]*payentity.Payment, error) {
	pos, err := r.data.DB.PayPayment(ctx).Query().
		Where(
			entpayment.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
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
		SetApplicationID(p.ApplicationID).
		SetMerchantID(p.MerchantID).
		SetChannel(int16(p.Channel)).
		SetNotifyURL(p.NotifyURL).
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
		if ent.IsConstraintError(err) {
			return nil, pay.ErrPaymentConflict
		}
		return nil, r.storeError(ctx, "pay.payment.create", err)
	}
	return toBizPayment(po), nil
}

func (r *paymentRepo) SetChannelPayload(ctx context.Context, req payrepo.SetChannelPayloadRequest) error {
	err := r.data.DB.PayPayment(ctx).UpdateOneID(req.ID).
		SetChannelPayload(req.Payload).
		Exec(ctx)
	if ent.IsNotFound(err) {
		return pay.ErrPaymentNotFound
	}
	if err != nil {
		return r.storeError(ctx, "pay.payment.set_channel_payload", err)
	}
	return nil
}

func (r *paymentRepo) SetFailReason(ctx context.Context, req payrepo.SetFailReasonRequest) error {
	err := r.data.DB.PayPayment(ctx).UpdateOneID(req.ID).
		SetFailReason(req.Reason).
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

func (r *paymentRepo) MarkPaid(ctx context.Context, req payrepo.MarkPaidRequest) (bool, error) {
	id := req.ID
	capture := req.Capture
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
	if n == 0 {
		// Zero rows is benign only when the row is already Paid. A PAID
		// assertion on a Failed/Closed/Expired row means the channel
		// captured money the ledger considers dead — surface it.
		if po, qerr := r.data.DB.PayPayment(ctx).Query().
			Where(entpayment.IDEQ(id)).
			Only(ctx); qerr == nil && po.Status != int16(payentity.PaymentPaid) {
			r.logger.WarnContext(ctx, "channel asserts PAID on a terminal payment",
				slog.String("operation", "pay.payment.mark_paid"),
				slog.Int64("payment_id", id),
				slog.Int("stored_status", int(po.Status)))
		}
	}
	return n > 0, nil
}

// BackfillPaidInfo completes payer evidence on an already-Paid row whose
// earlier confirmation lacked it — the one allowed write to a Paid
// payment besides the refund sum (design §5). The read-then-set fills
// exactly the still-empty fields: ANDed per-field CAS predicates would let
// one already-present field mask the others, and an all-empty capture
// would build an update with no setters.
func (r *paymentRepo) BackfillPaidInfo(ctx context.Context, req payrepo.BackfillPaidInfoRequest) (bool, error) {
	id := req.ID
	capture := req.Capture
	if capture.ChannelTradeNo == "" && capture.PayerID == "" && capture.PaidAt.IsZero() {
		return false, nil
	}
	po, err := r.data.DB.PayPayment(ctx).Query().
		Where(
			entpayment.IDEQ(id),
			entpayment.StatusEQ(int16(payentity.PaymentPaid)),
			entpayment.DeletedAtIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return false, nil // not Paid (or gone) — nothing to backfill
		}
		return false, r.storeError(ctx, "pay.payment.backfill_paid_info", err)
	}
	upd := r.data.DB.PayPayment(ctx).Update().
		Where(entpayment.IDEQ(id), entpayment.DeletedAtIsNil())
	changed := false
	if capture.ChannelTradeNo != "" && po.ChannelTradeNo == "" {
		upd.SetChannelTradeNo(capture.ChannelTradeNo)
		changed = true
	}
	if capture.PayerID != "" && po.PayerID == "" {
		upd.SetPayerID(capture.PayerID)
		changed = true
	}
	if !capture.PaidAt.IsZero() && po.PaidAt == nil {
		upd.SetPaidAt(capture.PaidAt)
		changed = true
	}
	if !changed {
		return false, nil // every field already present
	}
	if _, err := upd.Save(ctx); err != nil {
		return false, r.storeError(ctx, "pay.payment.backfill_paid_info", err)
	}
	return true, nil
}

func (r *paymentRepo) MarkFailed(ctx context.Context, req payrepo.MarkPaymentFailedRequest) (bool, error) {
	upd := r.data.DB.PayPayment(ctx).Update().
		Where(pendingPaymentScope(req.ID)...).
		SetStatus(int16(payentity.PaymentFailed)).
		SetUpdateBy(systemActor)
	if req.Reason != "" {
		upd.SetFailReason(truncateReason(req.Reason))
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
