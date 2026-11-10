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

// CreateRefundRequest is what a refund initiation states: the refund's
// shape plus the actor stamped into the audit fields. The caller holds
// the payment-scoped lock (pay:refund:{payment_id}) so the cap check and
// the insert are serialized against other refunds of the same payment.
type CreateRefundRequest struct {
	PaymentID int64
	Amount    int64
	Reason    string
	// ActorID stamps create_by/update_by (0 = the system itself, ADR 0002).
	ActorID int64
}

// PrepayRefundRequest is one freshly-recorded refund's channel call: the
// refund and the deployment's notify base URL the callback route is built
// from.
type PrepayRefundRequest struct {
	ID int64
	// NotifyBaseURL prefixes /pay/notify/{channel}/{merchant_id}.
	NotifyBaseURL string
}

// ApplyRefundStateRequest is one channel-asserted refund state to apply to
// one refund — the notify path's entry.
type ApplyRefundStateRequest struct {
	RefundNo int64
	State    ChannelRefundState
}

// Refund is the refund domain service: the only path to Refund rows. The
// cap — together, a payment's refunds never exceed what was paid — is
// checked here under the caller's lock; refund state advances are CAS
// from Pending and each success adds to the payment's refunded sum.
type Refund interface {
	Get(ctx context.Context, id int64) (*payentity.Refund, error)
	List(ctx context.Context, q listq.Query) ([]*payentity.Refund, error)
	// Create validates the cap and records the PENDING refund, copying the
	// Pay Application's refund result address. A missing or withdrawn Pay
	// Application or Merchant Account is not found. A disabled Pay
	// Application or Merchant Account is refused. The channel call happens
	// in PrepayRefund afterwards so a synchronous channel answer settles
	// the row immediately (design §5).
	Create(ctx context.Context, req CreateRefundRequest) (*payentity.Refund, error)
	// PrepayRefund asks the channel to move the money for one freshly
	// recorded refund. WeChat answers PENDING (the callback settles);
	// Alipay's synchronous answer settles here and now.
	PrepayRefund(ctx context.Context, req PrepayRefundRequest) (*payentity.Refund, error)
	// Query is the explicit settlement path for refunds whose callbacks
	// were lost.
	Query(ctx context.Context, id int64) (*payentity.Refund, error)
	// ApplyRefundState advances one refund per channel-asserted truth —
	// the notify path's entry. Every success adds to the payment's
	// refunded sum; the bool is false when the CAS found the refund
	// already settled (duplicate delivery, no-op by design).
	ApplyRefundState(ctx context.Context, req ApplyRefundStateRequest) (bool, error)
}

type refundImpl struct {
	refunds   payrepo.RefundRepo
	payments  payrepo.PaymentRepo
	apps      payrepo.PayApplicationRepo
	merchants payrepo.MerchantAccountRepo
	gateways  GatewayRegistry
	logger    *slog.Logger
}

// NewRefundService creates the refund domain service.
func NewRefundService(
	refunds payrepo.RefundRepo,
	payments payrepo.PaymentRepo,
	apps payrepo.PayApplicationRepo,
	merchants payrepo.MerchantAccountRepo,
	gateways GatewayRegistry,
	logger *slog.Logger,
) Refund {
	return &refundImpl{
		refunds:   refunds,
		payments:  payments,
		apps:      apps,
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

func (s *refundImpl) Create(ctx context.Context, req CreateRefundRequest) (*payentity.Refund, error) {
	if req.PaymentID <= 0 || req.Amount <= 0 || len(req.Reason) > 256 {
		err := pay.ErrPaymentInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	p, err := s.payments.FindByID(ctx, req.PaymentID)
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
	app, err := s.apps.FindByID(ctx, p.ApplicationID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	if app.Status != payentity.ApplicationEnabled {
		err = pay.ErrApplicationInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	m, err := s.merchants.FindByID(ctx, app.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	if m.Status != payentity.MerchantEnabled {
		err = pay.ErrMerchantInvalid
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
	if p.RefundedAmount+pending+req.Amount > p.Amount {
		err = pay.ErrRefundExceedLimit
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	rf := &payentity.Refund{
		ID:         s.refunds.NextID(),
		PaymentID:  p.ID,
		MerchantID: p.MerchantID,
		Channel:    p.Channel,
		NotifyURL:  app.RefundNotifyURL,
		Amount:     req.Amount,
		Reason:     req.Reason,
		Status:     payentity.RefundPending,
		CreateBy:   req.ActorID,
		UpdateBy:   req.ActorID,
	}
	created, err := s.refunds.Create(ctx, rf)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	return created, nil
}

func (s *refundImpl) PrepayRefund(ctx context.Context, req PrepayRefundRequest) (*payentity.Refund, error) {
	id := req.ID
	rf, err := s.refunds.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	if rf.Status != payentity.RefundPending {
		return rf, nil
	}
	resolved, err := s.resolve(ctx, rf.MerchantID)
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
	state, err := resolved.Gateway.Refund(ctx, RefundRequest{
		Merchant:       resolved.Merchant,
		RefundNo:       fmtPaymentNo(rf.ID),
		PaymentNo:      fmtPaymentNo(rf.PaymentID),
		ChannelTradeNo: payment.ChannelTradeNo,
		Total:          payment.Amount,
		Amount:         rf.Amount,
		Reason:         rf.Reason,
		NotifyURL:      notifyURL(req.NotifyBaseURL, rf.Channel, rf.MerchantID),
	})
	if err != nil {
		err = pay.ErrChannelError.WithCause(err)
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	if _, err := s.ApplyRefundState(ctx, ApplyRefundStateRequest{RefundNo: rf.ID, State: state}); err != nil {
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
	resolved, err := s.resolve(ctx, rf.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.query", err)
		return nil, err
	}
	state, err := resolved.Gateway.QueryRefund(ctx, RefundQueryRequest{
		Merchant:  resolved.Merchant,
		RefundNo:  fmtPaymentNo(rf.ID),
		PaymentNo: fmtPaymentNo(rf.PaymentID),
	})
	if err != nil {
		err = pay.ErrChannelError.WithCause(err)
		logx.Propagate(ctx, s.logger, "pay.svc.refund.query", err)
		return nil, err
	}
	if _, err := s.ApplyRefundState(ctx, ApplyRefundStateRequest{RefundNo: rf.ID, State: state}); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.query", err)
		return nil, err
	}
	return s.refunds.FindByID(ctx, id)
}

// ApplyRefundState advances one refund per channel-asserted truth. The
// settle and the payment's refunded-sum advance are one atomic repo write
// — the sum moves exactly once with the status, and a duplicate delivery
// loses the CAS and never double-counts.
func (s *refundImpl) ApplyRefundState(ctx context.Context, req ApplyRefundStateRequest) (bool, error) {
	refundNo, state := req.RefundNo, req.State
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
		advanced, err := s.refunds.MarkSuccess(ctx, payrepo.MarkRefundSuccessRequest{
			ID:              refundNo,
			PaymentID:       rf.PaymentID,
			Amount:          rf.Amount,
			ChannelRefundNo: state.ChannelRefundNo,
			RefundedAt:      state.RefundedAt,
		})
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.refund.apply_state", err)
			return false, err
		}
		return advanced, nil
	case payentity.RefundFailed:
		advanced, err := s.refunds.MarkFailed(ctx, payrepo.MarkRefundFailedRequest{ID: refundNo, Reason: state.FailReason})
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.refund.apply_state", err)
			return false, err
		}
		return advanced, nil
	default: // Pending — nothing to advance
		return false, nil
	}
}

func (s *refundImpl) resolve(ctx context.Context, merchantID int64) (*ResolvedGateway, error) {
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
