package service

import (
	"context"
	"log/slog"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// CreateRefundInput is what a refund initiation states. The caller holds
// the payment-scoped lock (pay:refund:{payment_id}) so the cap check and
// the insert are serialized against other refunds of the same payment.
type CreateRefundInput struct {
	PaymentID int64
	Amount    int64
	Reason    string
}

// Refund is the refund domain service: the only path to Refund rows. The
// cap — together, a payment's refunds never exceed what was paid — is
// checked here under the caller's lock; refund state advances are CAS
// from Pending and each success adds to the payment's refunded sum.
type Refund interface {
	Get(ctx context.Context, id int64) (*payentity.Refund, error)
	List(ctx context.Context, q listq.Query) ([]*payentity.Refund, error)
	// Create validates the cap and records the PENDING refund. The channel
	// call happens in PrepayRefund afterwards so a synchronous channel
	// answer settles the row immediately (design §5).
	Create(ctx context.Context, in *CreateRefundInput, actorID int64) (*payentity.Refund, error)
	// PrepayRefund asks the channel to move the money for one freshly
	// recorded refund. WeChat answers PENDING (the callback settles);
	// Alipay's synchronous answer settles here and now.
	PrepayRefund(ctx context.Context, id int64, notifyBaseURL string) (*payentity.Refund, error)
	// Query is the explicit settlement path for refunds whose callbacks
	// were lost.
	Query(ctx context.Context, id int64) (*payentity.Refund, error)
	// ApplyRefundState advances one refund per channel-asserted truth —
	// the notify path's entry. Every success adds to the payment's
	// refunded sum; the bool is false when the CAS found the refund
	// already settled (duplicate delivery, no-op by design).
	ApplyRefundState(ctx context.Context, refundNo int64, state ChannelRefundState) (bool, error)
}

type refundImpl struct {
	refunds   payrepo.RefundRepo
	payments  payrepo.PaymentRepo
	merchants payrepo.MerchantAccountRepo
	gateways  GatewayRegistry
	logger    *slog.Logger
}

// NewRefundService creates the refund domain service.
func NewRefundService(
	refunds payrepo.RefundRepo,
	payments payrepo.PaymentRepo,
	merchants payrepo.MerchantAccountRepo,
	gateways GatewayRegistry,
	logger *slog.Logger,
) Refund {
	return &refundImpl{
		refunds:   refunds,
		payments:  payments,
		merchants: merchants,
		gateways:  gateways,
		logger:    logger.With(slog.String("component", "domain.pay.refund.svc")),
	}
}

func (s *refundImpl) Get(ctx context.Context, id int64) (*payentity.Refund, error) {
	rf, err := s.refunds.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.get", err)
		return nil, err
	}
	return rf, nil
}

func (s *refundImpl) List(ctx context.Context, q listq.Query) ([]*payentity.Refund, error) {
	rs, err := s.refunds.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.list", err)
		return nil, err
	}
	return rs, nil
}

func (s *refundImpl) Create(ctx context.Context, in *CreateRefundInput, actorID int64) (*payentity.Refund, error) {
	if in.PaymentID <= 0 || in.Amount <= 0 || len(in.Reason) > 256 {
		err := pay.ErrPaymentInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	p, err := s.payments.FindByID(ctx, in.PaymentID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	// Refunds ride the payment's own state machine: only a PAID payment
	// carries refundable money (ticket 07).
	if p.Status != payentity.PaymentPaid {
		err = pay.ErrPaymentNotPaid
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	// The cap, under the caller's payment-scoped lock: settled + in-
	// flight + this ≤ paid. PENDING refunds reserve their amount — two
	// serialized async refunds of the same remainder must not both pass.
	pending, err := s.refunds.PendingRefundSum(ctx, p.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	if p.RefundedAmount+pending+in.Amount > p.Amount {
		err = pay.ErrRefundExceedLimit
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	rf := &payentity.Refund{
		ID:         s.refunds.NextID(),
		PaymentID:  p.ID,
		MerchantID: p.MerchantID,
		Channel:    p.Channel,
		Amount:     in.Amount,
		Reason:     in.Reason,
		Status:     payentity.RefundPending,
		CreateBy:   actorID,
		UpdateBy:   actorID,
	}
	created, err := s.refunds.Create(ctx, rf)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	return created, nil
}

func (s *refundImpl) PrepayRefund(ctx context.Context, id int64, notifyBaseURL string) (*payentity.Refund, error) {
	rf, err := s.refunds.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	if rf.Status != payentity.RefundPending {
		return rf, nil
	}
	m, gw, err := s.resolve(ctx, rf.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	// Channels whose refund API wants the original trade number and total
	// read them off the payment row — one read, errors propagate (a
	// refund must not ride on degraded evidence).
	payment, err := s.payments.FindByID(ctx, rf.PaymentID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	state, err := gw.Refund(ctx, RefundRequest{
		Merchant:       m,
		RefundNo:       fmtPaymentNo(rf.ID),
		PaymentNo:      fmtPaymentNo(rf.PaymentID),
		ChannelTradeNo: payment.ChannelTradeNo,
		Total:          payment.Amount,
		Amount:         rf.Amount,
		Reason:         rf.Reason,
		NotifyURL:      notifyURL(notifyBaseURL, rf.Channel, rf.MerchantID),
	})
	if err != nil {
		err = pay.ErrChannelError.WithCause(err)
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	if _, err := s.ApplyRefundState(ctx, rf.ID, state); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	rf, err = s.refunds.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	return rf, nil
}

func (s *refundImpl) Query(ctx context.Context, id int64) (*payentity.Refund, error) {
	rf, err := s.refunds.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.query", err)
		return nil, err
	}
	if rf.Status != payentity.RefundPending {
		return rf, nil
	}
	m, gw, err := s.resolve(ctx, rf.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.query", err)
		return nil, err
	}
	state, err := gw.QueryRefund(ctx, m, fmtPaymentNo(rf.ID), fmtPaymentNo(rf.PaymentID))
	if err != nil {
		err = pay.ErrChannelError.WithCause(err)
		logx.Propagate(ctx, s.logger, "pay.svc.refund.query", err)
		return nil, err
	}
	if _, err := s.ApplyRefundState(ctx, rf.ID, state); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.query", err)
		return nil, err
	}
	return s.refunds.FindByID(ctx, id)
}

// ApplyRefundState advances one refund per channel-asserted truth. The
// settle and the payment's refunded-sum advance are one atomic repo write
// — the sum moves exactly once with the status, and a duplicate delivery
// loses the CAS and never double-counts.
func (s *refundImpl) ApplyRefundState(ctx context.Context, refundNo int64, state ChannelRefundState) (bool, error) {
	switch state.State {
	case payentity.RefundSuccess:
		rf, err := s.refunds.FindByID(ctx, refundNo)
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.refund.apply_state", err)
			return false, err
		}
		// Amount verification before the CAS: a signature-valid success
		// naming a different amount for our out_refund_no never settles.
		// Zero means the confirmation asserted no amount and skips the
		// check (the adapters always populate it on Success).
		if state.RefundAmount > 0 && rf.Amount != state.RefundAmount {
			s.logger.Warn("channel-asserted refund amount differs from the refund; refusing to settle",
				slog.String("operation", "pay.svc.refund.apply_state"),
				slog.Int64("refund_id", refundNo),
				slog.Int64("stored_amount", rf.Amount),
				slog.Int64("asserted_amount", state.RefundAmount))
			return false, nil
		}
		// The settle and the refunded-sum advance are one atomic unit in
		// the repo — the sum moves exactly once with the status.
		advanced, err := s.refunds.MarkSuccess(ctx, refundNo, rf.PaymentID, rf.Amount, state.ChannelRefundNo, state.RefundedAt)
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.refund.apply_state", err)
			return false, err
		}
		return advanced, nil
	case payentity.RefundFailed:
		advanced, err := s.refunds.MarkFailed(ctx, refundNo, state.FailReason)
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.refund.apply_state", err)
			return false, err
		}
		return advanced, nil
	default: // Pending — nothing to advance
		return false, nil
	}
}

func (s *refundImpl) resolve(ctx context.Context, merchantID int64) (*payentity.MerchantAccount, ChannelGateway, error) {
	return resolveMerchantGateway(ctx, s.merchants, s.gateways, merchantID)
}

// channelTradeNo and totalAmount read the payment's snapshot for channels
// whose refund API wants them; missing evidence degrades to empty — the
// channels resolve by merchant order number anyway.
func (s *refundImpl) channelTradeNo(ctx context.Context, paymentID int64) string {
	p, err := s.payments.FindByID(ctx, paymentID)
	if err != nil {
		return ""
	}
	return p.ChannelTradeNo
}

func (s *refundImpl) totalAmount(ctx context.Context, paymentID int64) int64 {
	p, err := s.payments.FindByID(ctx, paymentID)
	if err != nil {
		return 0
	}
	return p.Amount
}
