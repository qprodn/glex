package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// defaultExpireMinutes is the initiation expiry when the caller states none.
const defaultExpireMinutes = 30

// maxExpireMinutes caps one payment's validity window.
const maxExpireMinutes = 24 * 60

// CreatePaymentRequest is what an initiation use case states: the payment's
// shape plus the actor stamped into the audit fields.
type CreatePaymentRequest struct {
	MerchantID    int64
	BizNo         string
	Subject       string
	Amount        int64
	PayMethod     payentity.PayMethod
	ExpireMinutes int32
	// ActorID stamps create_by/update_by (0 = the system itself, ADR 0002).
	ActorID int64
}

// GetByBizNoRequest names one merchant's Business Order Number — the pair
// a newest-Payment-by-outcome read resolves.
type GetByBizNoRequest struct {
	MerchantID int64
	BizNo      string
}

// PrepayPaymentRequest is one freshly-recorded PENDING payment's channel
// prepay: the payment and the deployment's notify base URL the callback
// route is built from.
type PrepayPaymentRequest struct {
	ID int64
	// NotifyBaseURL prefixes /pay/notify/{channel}/{merchant_id}.
	NotifyBaseURL string
}

// ClosePaymentRequest is one manual close: the payment and the actor
// stamped into the audit fields.
type ClosePaymentRequest struct {
	ID int64
	// ActorID stamps update_by (0 = the system itself, ADR 0002).
	ActorID int64
}

// ApplyChannelStateRequest is one channel-asserted payment state to apply
// to one payment — the notify path's entry (ticket 06).
type ApplyChannelStateRequest struct {
	PaymentNo int64
	State     ChannelPaymentState
}

// Payment is the payment domain service: the only path to Payment rows and
// the settlement seam where channel-asserted truth is applied to them by
// CAS (terminal states irreversible, out-of-order receipts no-ops).
type Payment interface {
	Get(ctx context.Context, id int64) (*payentity.Payment, error)
	// GetByBizNo resolves a merchant's newest Payment for a Business
	// Order Number, settling it first when PENDING and expired — the
	// caller reads an outcome, so the lazy-expiry rule applies here too.
	GetByBizNo(ctx context.Context, req GetByBizNoRequest) (*payentity.Payment, error)
	List(ctx context.Context, q listq.Query) ([]*payentity.Payment, error)
	// Initiate validates the initiation against the Merchant Account,
	// closes the merchant's still-pending payments for the Business Order
	// Number, and records the new PENDING payment. The caller owns the
	// prepay lock and the ambient transaction (close + insert is one
	// indivisible batch); the channel prepay runs afterwards, outside it.
	Initiate(ctx context.Context, req CreatePaymentRequest) (*payentity.Payment, error)
	// Prepay contacts the channel for one freshly-recorded PENDING payment
	// and backfills the QR payload; a channel rejection fails the payment
	// with the reason — no payable illusion survives (ticket 05).
	Prepay(ctx context.Context, req PrepayPaymentRequest) (*payentity.Payment, error)
	// Query is the explicit settlement path: it asks the channel for the
	// payment's true state and advances by CAS — the fallback when
	// callbacks are lost.
	Query(ctx context.Context, id int64) (*payentity.Payment, error)
	// SweepExpired proactively converges up to limit still-Pending payments
	// whose expiry has passed — the expire sweep's body (ADR 0023). One
	// row's failure warns and the batch continues; the lazy paths still
	// heal anything left behind. Returns how many candidates settled.
	SweepExpired(ctx context.Context, limit int) (int, error)
	// Close manually closes a PENDING payment. A channel "already paid"
	// answer flips into a query and settles PAID; late close intents on
	// terminal payments are no-ops.
	Close(ctx context.Context, req ClosePaymentRequest) (*payentity.Payment, error)
	// ApplyChannelState advances one payment per channel-asserted state —
	// the notify path's entry (ticket 06). The bool is false when the CAS
	// found the payment already settled (duplicate or out-of-order).
	ApplyChannelState(ctx context.Context, req ApplyChannelStateRequest) (bool, error)
}

type paymentImpl struct {
	payments  payrepo.PaymentRepo
	merchants payrepo.MerchantAccountRepo
	gateways  GatewayRegistry
	logger    *slog.Logger
}

// NewPaymentService creates the payment domain service.
func NewPaymentService(
	payments payrepo.PaymentRepo,
	merchants payrepo.MerchantAccountRepo,
	gateways GatewayRegistry,
	logger *slog.Logger,
) Payment {
	return &paymentImpl{
		payments:  payments,
		merchants: merchants,
		gateways:  gateways,
		logger:    logger.With(slog.String("component", "domain.pay.payment.svc")),
	}
}

// resolve prepares the merchant's gateway: find, decrypt, resolve. Every
// channel-touching path funnels through here.
func (s *paymentImpl) resolve(ctx context.Context, merchantID int64) (*ResolvedGateway, error) {
	return resolveMerchantGateway(ctx, s.merchants, s.gateways, merchantID)
}

// resolveMerchantGateway is the shared find-decrypt-resolve seam for every
// channel-touching path (payments, refunds, notification intake).
func resolveMerchantGateway(ctx context.Context, merchants payrepo.MerchantAccountRepo, gateways GatewayRegistry, merchantID int64) (*ResolvedGateway, error) {
	m, err := merchants.FindByIDAny(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if err := merchants.LoadConfig(ctx, m); err != nil {
		return nil, err
	}
	gw, err := gateways.Resolve(ctx, m)
	if err != nil {
		return nil, err
	}
	return &ResolvedGateway{Merchant: m, Gateway: gw}, nil
}

func (s *paymentImpl) Get(ctx context.Context, id int64) (*payentity.Payment, error) {
	p, err := s.payments.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.get", err)
		return nil, err
	}
	// Lazy expiry: a PENDING payment past its window settles on the read
	// path; the settlement is best-effort — a channel failure degrades to
	// the stored row, never fails the read (write-on-read by design).
	if p.Status == payentity.PaymentPending && time.Now().After(p.ExpireTime) {
		if _, err := s.settle(ctx, p); err != nil {
			s.logger.Warn("settle on read failed; serving stored state",
				slog.String("operation", "pay.svc.payment.get"),
				slog.Int64("payment_id", p.ID),
				slog.Any("error", err))
		}
		p, err = s.payments.FindByID(ctx, id)
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.payment.get", err)
			return nil, err
		}
	}
	return p, nil
}

func (s *paymentImpl) GetByBizNo(ctx context.Context, req GetByBizNoRequest) (*payentity.Payment, error) {
	p, err := s.payments.FindLatestByBizNo(ctx, payrepo.FindLatestByBizNoRequest{MerchantID: req.MerchantID, BizNo: req.BizNo})
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.get_by_biz_no", err)
		return nil, err
	}
	if p.Status == payentity.PaymentPending && time.Now().After(p.ExpireTime) {
		if _, err := s.settle(ctx, p); err != nil {
			s.logger.Warn("settle on read failed; serving stored state",
				slog.String("operation", "pay.svc.payment.get_by_biz_no"),
				slog.Int64("payment_id", p.ID),
				slog.Any("error", err))
		}
		p, err = s.payments.FindByID(ctx, p.ID)
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.payment.get_by_biz_no", err)
			return nil, err
		}
	}
	return p, nil
}

func (s *paymentImpl) List(ctx context.Context, q listq.Query) ([]*payentity.Payment, error) {
	ps, err := s.payments.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.list", err)
		return nil, err
	}
	return ps, nil
}

func (s *paymentImpl) Initiate(ctx context.Context, req CreatePaymentRequest) (*payentity.Payment, error) {
	initiation := &payentity.Initiation{
		MerchantID:    req.MerchantID,
		BizNo:         req.BizNo,
		Subject:       req.Subject,
		Amount:        req.Amount,
		Currency:      "CNY",
		PayMethod:     req.PayMethod,
		ExpireMinutes: req.ExpireMinutes,
	}
	if err := initiation.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.initiate", err)
		return nil, err
	}
	m, err := s.merchants.FindByID(ctx, req.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.initiate", err)
		return nil, err
	}
	if m.Status != payentity.MerchantEnabled {
		err = pay.ErrMerchantInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.payment.initiate", err)
		return nil, err
	}
	// Re-initiation: the merchant's still-pending payments for this
	// Business Order Number close as part of the same batch.
	if _, err := s.payments.ClosePendingByBizNo(ctx, payrepo.ClosePendingByBizNoRequest{
		MerchantID: req.MerchantID, BizNo: req.BizNo, ActorID: req.ActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.initiate", err)
		return nil, err
	}
	minutes := req.ExpireMinutes
	if minutes <= 0 {
		minutes = defaultExpireMinutes
	}
	if minutes > maxExpireMinutes {
		minutes = maxExpireMinutes
	}
	p := &payentity.Payment{
		ID:         s.payments.NextID(),
		MerchantID: req.MerchantID,
		Channel:    m.Channel,
		BizNo:      req.BizNo,
		Subject:    req.Subject,
		Amount:     req.Amount,
		Currency:   "CNY",
		PayMethod:  req.PayMethod,
		Status:     payentity.PaymentPending,
		ExpireTime: time.Now().Add(time.Duration(minutes) * time.Minute),
		CreateBy:   req.ActorID,
		UpdateBy:   req.ActorID,
	}
	created, err := s.payments.Create(ctx, p)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.initiate", err)
		return nil, err
	}
	return created, nil
}

func (s *paymentImpl) Prepay(ctx context.Context, req PrepayPaymentRequest) (*payentity.Payment, error) {
	id := req.ID
	p, err := s.payments.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.prepay", err)
		return nil, err
	}
	if p.Status != payentity.PaymentPending {
		// Lost a race against a close/expire between insert and prepay —
		// the payment stays settled, no channel call is made.
		return p, nil
	}
	resolved, err := s.resolve(ctx, p.MerchantID)
	if err != nil {
		s.failPending(ctx, id, err)
		logx.Propagate(ctx, s.logger, "pay.svc.payment.prepay", err)
		return nil, err
	}
	result, err := resolved.Gateway.Prepay(ctx, PrepayRequest{
		Merchant:  resolved.Merchant,
		PaymentNo: fmtPaymentNo(p.ID),
		Amount:    p.Amount,
		Currency:  p.Currency,
		Subject:   p.Subject,
		ExpireAt:  p.ExpireTime,
		Method:    p.PayMethod,
		NotifyURL: notifyURL(req.NotifyBaseURL, p.Channel, p.MerchantID),
	})
	if err != nil {
		s.failPending(ctx, id, err)
		err = pay.ErrChannelError.WithCause(err)
		logx.Propagate(ctx, s.logger, "pay.svc.payment.prepay", err)
		return nil, err
	}
	if err := s.payments.SetChannelPayload(ctx, payrepo.SetChannelPayloadRequest{ID: id, Payload: result.ChannelPayload}); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.prepay", err)
		return nil, err
	}
	p, err = s.payments.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.prepay", err)
		return nil, err
	}
	return p, nil
}

func (s *paymentImpl) Query(ctx context.Context, id int64) (*payentity.Payment, error) {
	p, err := s.payments.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.query", err)
		return nil, err
	}
	if p.Status == payentity.PaymentPending {
		if _, err := s.settle(ctx, p); err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.payment.query", err)
			return nil, err
		}
		p, err = s.payments.FindByID(ctx, id)
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.payment.query", err)
			return nil, err
		}
	}
	return p, nil
}

// SweepExpired is the expire sweep's body (ADR 0023): proactive
// convergence of the lazy settlement paths — a payment nobody reads still
// reaches its terminal state. Candidates settle through the same settle()
// the read paths use, already-paid race included. Per-row failures Warn and
// the run pages past them (IDGT cursor): a poisoned head — rows that fail
// permanently, e.g. a merchant that no longer resolves — must not
// monopolize every batch, and the page budget bounds one run's channel
// traffic at pages×limit. A dead context (run timeout, lock loss) stops
// the sweep and propagates, so the job log records the interruption
// instead of masking it as success. A PAID landing here still publishes no
// PaymentCompleted — the documented contract holds: query-settle settles
// silently, absence of the event is not absence of payment.
func (s *paymentImpl) SweepExpired(ctx context.Context, limit int) (int, error) {
	const sweepPageBudget = 10
	settled := 0
	var after int64
	for page := 0; page < sweepPageBudget && settled < limit; page++ {
		candidates, err := s.payments.ListPendingExpired(ctx, payrepo.ListPendingExpiredRequest{Before: time.Now(), AfterID: after, Limit: limit})
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.payment.sweep_expired", err)
			return settled, err
		}
		if len(candidates) == 0 {
			return settled, nil
		}
		for _, p := range candidates {
			after = p.ID
			if err := ctx.Err(); err != nil {
				// The run itself is dead (timeout or lock loss): stop
				// without Warn-flooding the remaining rows and let the
				// interrupted run land as a failure in its job log.
				logx.Propagate(ctx, s.logger, "pay.svc.payment.sweep_expired", err)
				return settled, err
			}
			if _, err := s.settle(ctx, p); err != nil {
				// Degraded, not propagated: the sweep continues and the
				// lazy paths still heal this row on its next read (ADR
				// 0007's handling site; a Warn, because the sweep's own
				// outcome is unaffected).
				s.logger.Warn("expire sweep could not settle a payment; lazy paths still heal it",
					slog.String("operation", "pay.svc.payment.sweep_expired"),
					slog.Int64("payment_id", p.ID),
					slog.Any("error", err))
				continue
			}
			settled++
		}
	}
	return settled, nil
}

func (s *paymentImpl) Close(ctx context.Context, req ClosePaymentRequest) (*payentity.Payment, error) {
	id := req.ID
	p, err := s.payments.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.close", err)
		return nil, err
	}
	// Terminal states are irreversible — a late close intent is a no-op,
	// not an error (ticket 05).
	if p.Status != payentity.PaymentPending {
		return p, nil
	}
	resolved, err := s.resolve(ctx, p.MerchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.close", err)
		return nil, err
	}
	closeErr := resolved.Gateway.Close(ctx, CloseRequest{Merchant: resolved.Merchant, PaymentNo: fmtPaymentNo(p.ID)})
	if closeErr != nil {
		if !isChannelAlreadyPaid(closeErr) {
			err = pay.ErrChannelError.WithCause(closeErr)
			logx.Propagate(ctx, s.logger, "pay.svc.payment.close", err)
			return nil, err
		}
		// The channel says paid: flip into a query and settle PAID.
		state, queryErr := resolved.Gateway.Query(ctx, QueryRequest{Merchant: resolved.Merchant, PaymentNo: fmtPaymentNo(p.ID)})
		if queryErr != nil {
			err = pay.ErrChannelError.WithCause(queryErr)
			logx.Propagate(ctx, s.logger, "pay.svc.payment.close", err)
			return nil, err
		}
		if _, err := s.ApplyChannelState(ctx, ApplyChannelStateRequest{PaymentNo: p.ID, State: state}); err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.payment.close", err)
			return nil, err
		}
		return s.payments.FindByID(ctx, id)
	}
	if _, err := s.payments.MarkClosed(ctx, p.ID); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.close", err)
		return nil, err
	}
	p, err = s.payments.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.close", err)
		return nil, err
	}
	return p, nil
}

// ApplyChannelState advances one payment per channel-asserted truth. Every
// transition is a CAS from Pending; false returns are duplicates,
// out-of-order receipts, or a rejected amount mismatch — no-ops by design,
// never errors.
func (s *paymentImpl) ApplyChannelState(ctx context.Context, req ApplyChannelStateRequest) (bool, error) {
	paymentNo, state := req.PaymentNo, req.State
	switch state.State {
	case payentity.PaymentPaid:
		// Amount verification before the CAS: a signature-valid success
		// naming a different amount for our out_trade_no never settles.
		// Zero means the confirmation asserted no amount and skips the
		// check (the adapters always populate it on Paid).
		if state.Amount > 0 {
			p, err := s.payments.FindByID(ctx, paymentNo)
			if err != nil {
				if errors.Is(err, pay.ErrPaymentNotFound) {
					return false, nil
				}
				logx.Propagate(ctx, s.logger, "pay.svc.payment.apply_state", err)
				return false, err
			}
			if p.Amount != state.Amount {
				s.logger.Warn("channel-asserted paid amount differs from the payment; refusing to settle",
					slog.String("operation", "pay.svc.payment.apply_state"),
					slog.Int64("payment_id", paymentNo),
					slog.Int64("stored_amount", p.Amount),
					slog.Int64("asserted_amount", state.Amount))
				return false, nil
			}
		}
		capture := payentity.PaidCapture{
			ChannelTradeNo: state.ChannelTradeNo,
			PayerID:        state.PayerID,
			PaidAt:         state.PaidAt,
			Amount:         state.Amount,
		}
		advanced, err := s.payments.MarkPaid(ctx, payrepo.MarkPaidRequest{ID: paymentNo, Capture: capture})
		if err != nil {
			logx.Propagate(ctx, s.logger, "pay.svc.payment.apply_state", err)
			return false, err
		}
		if !advanced {
			// Already PAID: backfill any evidence the earlier confirmation
			// lacked; already FAILED/CLOSED/EXPIRED: irreversible no-op.
			if _, err := s.payments.BackfillPaidInfo(ctx, payrepo.BackfillPaidInfoRequest{ID: paymentNo, Capture: capture}); err != nil {
				logx.Propagate(ctx, s.logger, "pay.svc.payment.apply_state", err)
				return false, err
			}
		}
		return advanced, nil
	case payentity.PaymentFailed:
		return s.applyTransition(ctx, paymentNo, func() (bool, error) {
			return s.payments.MarkFailed(ctx, payrepo.MarkPaymentFailedRequest{ID: paymentNo, Reason: state.FailReason})
		})
	case payentity.PaymentClosed:
		return s.applyTransition(ctx, paymentNo, func() (bool, error) {
			return s.payments.MarkClosed(ctx, paymentNo)
		})
	case payentity.PaymentExpired:
		return s.applyTransition(ctx, paymentNo, func() (bool, error) {
			return s.payments.MarkExpired(ctx, paymentNo)
		})
	default: // Pending — nothing to advance
		return false, nil
	}
}

func (s *paymentImpl) applyTransition(ctx context.Context, paymentNo int64, advance func() (bool, error)) (bool, error) {
	advanced, err := advance()
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.payment.apply_state", err)
		return false, err
	}
	return advanced, nil
}

// settle trues one PENDING payment up against the channel: channel PAID
// wins (callback lost); channel PENDING past expiry closes at the channel
// (best-effort) and marks EXPIRED locally; other states apply verbatim.
func (s *paymentImpl) settle(ctx context.Context, p *payentity.Payment) (bool, error) {
	resolved, err := s.resolve(ctx, p.MerchantID)
	if err != nil {
		return false, err
	}
	gw, m := resolved.Gateway, resolved.Merchant
	state, err := gw.Query(ctx, QueryRequest{Merchant: m, PaymentNo: fmtPaymentNo(p.ID)})
	if err != nil {
		return false, err
	}
	if state.State == payentity.PaymentPending && time.Now().After(p.ExpireTime) {
		// Expired with no channel-side payment: close at the channel
		// best-effort (the channel may have auto-closed already) and
		// settle locally as EXPIRED. An "already paid" close answer means
		// the user paid between the Query and the Close — money was
		// captured, so re-query and apply the channel's truth instead of
		// forcing EXPIRED (captured funds must land as PAID, refundable).
		if closeErr := gw.Close(ctx, CloseRequest{Merchant: m, PaymentNo: fmtPaymentNo(p.ID)}); closeErr != nil {
			if isChannelAlreadyPaid(closeErr) {
				paidState, queryErr := gw.Query(ctx, QueryRequest{Merchant: m, PaymentNo: fmtPaymentNo(p.ID)})
				if queryErr != nil {
					return false, queryErr
				}
				return s.ApplyChannelState(ctx, ApplyChannelStateRequest{PaymentNo: p.ID, State: paidState})
			}
			s.logger.Warn("channel close of expired payment failed; expiring locally",
				slog.String("operation", "pay.svc.payment.settle"),
				slog.Int64("payment_id", p.ID),
				slog.Any("error", closeErr))
		}
		state.State = payentity.PaymentExpired
	}
	return s.ApplyChannelState(ctx, ApplyChannelStateRequest{PaymentNo: p.ID, State: state})
}

// fmtPaymentNo renders the snowflake ID as the channel-facing order
// number (out_trade_no).
func fmtPaymentNo(id int64) string {
	return strconv.FormatInt(id, 10)
}

// notifyURL builds the absolute callback URL a prepay carries:
// {base}/pay/notify/{channel}/{merchant_id} (ADR 0020).
func notifyURL(base string, channel payentity.Channel, merchantID int64) string {
	return fmt.Sprintf("%s/pay/notify/%s/%d", strings.TrimRight(base, "/"), channel.Code(), merchantID)
}

// isChannelAlreadyPaid reports whether err is the gateway's already-paid
// flip signal.
func isChannelAlreadyPaid(err error) bool {
	return errors.Is(err, ErrChannelAlreadyPaid)
}

// failPending records why a pending payment died. The CAS losing means the
// payment already settled — the reason is then pointless, which the error
// path tolerates.
func (s *paymentImpl) failPending(ctx context.Context, id int64, cause error) {
	if _, err := s.payments.MarkFailed(ctx, payrepo.MarkPaymentFailedRequest{ID: id, Reason: truncateMessage(cause.Error(), 256)}); err != nil {
		s.logger.Warn("failed to record payment failure",
			slog.String("operation", "pay.svc.payment.fail_pending"),
			slog.Int64("payment_id", id),
			slog.Any("error", err))
	}
}
