package service

import (
	"context"
	"fmt"
	"log/slog"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/logx"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
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
	List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*payentity.Refund, error)
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
	refunds  payrepo.RefundRepo
	payments payrepo.PaymentRepo
	merchants payrepo.MerchantAccountRepo
	gateways GatewayRegistry
	logger   *slog.Logger
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

func (s *refundImpl) List(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*payentity.Refund, error) {
	rs, err := s.refunds.List(ctx,
		payrepo.ListFilter(filter),
		payrepo.ListOrderBy(orderBy),
		payrepo.ListLimit(limit),
		payrepo.ListOffset(offset),
	)
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
	// The cap, under the caller's payment-scoped lock: refunded + this ≤
	// paid. The SQL sum accumulates atomically afterwards; the lock is
	// what serializes the check.
	if p.RefundedAmount+in.Amount > p.Amount {
		err = pay.ErrRefundExceedLimit
		logx.Propagate(ctx, s.logger, "pay.svc.refund.create", err)
		return nil, err
	}
	rf := &payentity.Refund{
		ID:        s.refunds.NextID(),
		PaymentID: p.ID,
		MerchantID: p.MerchantID,
		Channel:   p.Channel,
		Amount:    in.Amount,
		Reason:    in.Reason,
		Status:    payentity.RefundPending,
		CreateBy:  actorID,
		UpdateBy:  actorID,
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
	p, gw, err := s.resolve(ctx, rf.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.prepay", err)
		return nil, err
	}
	state, err := gw.Refund(ctx, RefundRequest{
		Merchant:       p,
		RefundNo:       strconvInt64(rf.ID),
		PaymentNo:      strconvInt64(rf.PaymentID),
		ChannelTradeNo: s.channelTradeNo(ctx, rf.PaymentID),
		Total:          s.totalAmount(ctx, rf.PaymentID),
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
	_, gw, err := s.resolve(ctx, rf.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.refund.query", err)
		return nil, err
	}
	state, err := gw.QueryRefund(ctx, &payentity.MerchantAccount{}, strconvInt64(rf.ID))
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

// ApplyRefundState advances one refund per channel-asserted truth. A
// successful CAS adds to the payment's refunded sum — the sum and the
// refund status move together, idempotently: a duplicate delivery loses
// the CAS and never double-counts.
func (s *refundImpl) ApplyRefundState(ctx context.Context, refundNo int64, state ChannelRefundState) (bool, error) {
	switch state.State {
	case payentity.RefundSuccess:
		rf, err := s.refunds.FindByID(ctx, refundNo)
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.refund.apply_state", err)
			return false, err
		}
		advanced, err := s.refunds.MarkSuccess(ctx, refundNo, state.ChannelRefundNo, state.RefundedAt)
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.refund.apply_state", err)
			return false, err
		}
		if advanced {
			// The refund status moved Pending → Success exactly once, so
			// the payment's refunded sum advances exactly once with it.
			if _, err := s.payments.AccumulateRefunded(ctx, rf.PaymentID, rf.Amount); err != nil {
				logx.Propagate(ctx, s.logger, "pay.svc.refund.apply_state", err)
				return false, err
			}
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
	m, err := s.merchants.FindByID(ctx, merchantID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.merchants.LoadConfig(ctx, m); err != nil {
		return nil, nil, err
	}
	gw, err := s.gateways.Resolve(ctx, m)
	if err != nil {
		return nil, nil, err
	}
	return m, gw, nil
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

// strconvInt64 renders a snowflake ID as the channel-facing number.
func strconvInt64(id int64) string {
	return fmt.Sprintf("%d", id)
}
