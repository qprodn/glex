package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/pkg/logx"
)

// ChannelNotificationRequest is one channel delivery: the route's channel
// and merchant, plus the raw HTTP evidence (ADR 0020, ADR 0027).
type ChannelNotificationRequest struct {
	Channel    payentity.Channel
	MerchantID int64
	Headers    http.Header
	Body       []byte
}

// ChannelNotificationResult is one intake's answer. Processed reports
// whether this delivery landed a state change — a duplicate whose CAS is
// a no-op is false and still ACKed. AckBody is the channel's dialect
// answer; the HTTP handler writes it verbatim after durable processing.
type ChannelNotificationResult struct {
	Processed bool
	AckBody   string
}

// CompletedPublisher is the pay module's port for a Payment Completed
// body. The composition root adapts eventbus.Producer onto it; this
// package does not import eventbus (ADR 0027).
type CompletedPublisher interface {
	Publish(ctx context.Context, body []byte) error
}

// Intake accepts one Channel Notification: resolve the merchant gateway,
// verify, record the write-once evidence, bind the target to the
// verifying merchant, advance by CAS, and return the channel's ACK
// (ADR 0020, ADR 0027).
type Intake interface {
	Accept(ctx context.Context, req *ChannelNotificationRequest) (*ChannelNotificationResult, error)
}

type intakeImpl struct {
	merchants Merchant
	payments  Payment
	refunds   Refund
	logs      payrepo.NotifyLogRepo
	publisher CompletedPublisher
	logger    *slog.Logger
}

// NewIntake creates the channel-notification intake.
func NewIntake(
	merchants Merchant,
	payments Payment,
	refunds Refund,
	logs payrepo.NotifyLogRepo,
	publisher CompletedPublisher,
	logger *slog.Logger,
) Intake {
	return &intakeImpl{
		merchants: merchants,
		payments:  payments,
		refunds:   refunds,
		logs:      logs,
		publisher: publisher,
		logger:    logger.With(slog.String("component", "domain.pay.notify.svc")),
	}
}

// Accept runs the intake pipeline. Verification failures record unverified
// evidence and return ErrNotifyInvalid. The evidence insert is the
// durability gate: without it the state advance does not run. A redelivery
// runs the advance again — every transition is an idempotent CAS — so a
// delivery whose first advance failed can finish, and a repeat applies no
// second state change. The dedup bool from the insert is deliberately
// unused for that reason.
func (s *intakeImpl) Accept(ctx context.Context, req *ChannelNotificationRequest) (*ChannelNotificationResult, error) {
	channel, merchantID, headers, body := req.Channel, req.MerchantID, req.Headers, req.Body
	if !channel.Valid() {
		err := pay.ErrNotifyInvalid
		logx.Propagate(ctx, s.logger, "pay.svc.notify.accept", err)
		return nil, err
	}
	resolved, err := s.merchants.ResolveGateway(ctx, merchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notify.accept", err)
		return nil, err
	}
	notif, parseErr := resolved.Gateway.ParseNotification(ctx, NotificationRequest{
		Merchant: resolved.Merchant,
		Headers:  headers,
		Body:     body,
	})
	notif.MerchantID = merchantID
	notif.ID = s.logs.NextID()
	if len(notif.SignHeaders) == 0 {
		notif.SignHeaders = signHeaderSnapshot(headers)
	}
	notif.RawBody = string(body)
	if parseErr != nil {
		// The evidence row for a rejected delivery: verified=false,
		// nothing advanced. A redelivered duplicate garbage payload just
		// loses the insert race — the rejection is unchanged.
		notif.Verified = false
		if _, insertErr := s.logs.Insert(ctx, &notif); insertErr != nil {
			s.logger.Warn("unverified notify evidence insert failed",
				slog.String("operation", "pay.svc.notify.accept"),
				slog.Any("error", insertErr))
		}
		err = pay.ErrNotifyInvalid.WithCause(parseErr)
		logx.Propagate(ctx, s.logger, "pay.svc.notify.accept", err)
		return nil, err
	}
	notif.Verified = true
	if _, err := s.logs.Insert(ctx, &notif); err != nil {
		logx.Propagate(ctx, s.logger, "pay.svc.notify.accept", err)
		return nil, err
	}
	processed, err := s.advance(ctx, &notif)
	if err != nil {
		// The advance failed — no ACK, the channel retries, and the next
		// delivery (duplicate or not) re-attempts the advance.
		logx.Propagate(ctx, s.logger, "pay.svc.notify.advance", err)
		return nil, err
	}
	if err := s.logs.MarkProcessed(ctx, notif.ID); err != nil {
		// The flag is diagnostic; the CAS advance is the guarantee (the
		// handling site, ADR 0007).
		s.logger.Warn("notify processed-flag update failed",
			slog.String("operation", "pay.svc.notify.accept"),
			slog.Int64("notify_log_id", notif.ID),
			slog.Any("error", err))
	}
	return &ChannelNotificationResult{Processed: processed, AckBody: resolved.Gateway.AckSuccess()}, nil
}

// advancePayment runs the payment advance and, when the transition into
// the paid state actually landed, announces Payment Completed. Query,
// close, and expire-sweep do not publish (the Payment stays the money truth).
func (s *intakeImpl) advancePayment(
	ctx context.Context,
	p *payentity.Payment,
	notif *payentity.ChannelNotification,
	targetNo int64,
) (bool, error) {
	advanced, err := s.payments.ApplyChannelState(ctx, ApplyChannelStateRequest{
		PaymentNo: targetNo,
		State: ChannelPaymentState{
			State:          notif.PaymentState,
			ChannelTradeNo: notif.ChannelTradeNo,
			PayerID:        notif.PayerID,
			PaidAt:         notif.PaidAt,
			Amount:         notif.Amount,
		},
	})
	if err != nil {
		return false, err
	}
	if advanced && notif.PaymentState == payentity.PaymentPaid {
		s.publishPaymentCompleted(ctx, p, notif)
	}
	return advanced, nil
}

// publishPaymentCompleted announces the completed payment. Publishing is
// best-effort and never fails the intake: the state change is already
// durable and the channel must still be ACKed (ADR 0022).
func (s *intakeImpl) publishPaymentCompleted(ctx context.Context, p *payentity.Payment, notif *payentity.ChannelNotification) {
	event := payentity.NewPaymentCompletedEvent(s.logs.NextID(), p, notif)
	body, err := json.Marshal(event)
	if err != nil {
		s.logger.Warn("payment completed event marshal failed",
			slog.String("operation", "pay.svc.notify.publish_completed"),
			slog.Int64("payment_id", p.ID),
			slog.Any("error", err))
		return
	}
	if err := s.publisher.Publish(ctx, body); err != nil {
		s.logger.Warn("payment completed publish degraded",
			slog.String("operation", "pay.svc.notify.publish_completed"),
			slog.Int64("payment_id", p.ID),
			slog.Any("error", err))
	}
}

// advance applies the verified event to its target row.
//
// The target is first bound to the merchant whose gateway verified the
// delivery: signature verification only proves the channel signed an event
// for notif.MerchantID's account — the asserted out_trade_no/out_refund_no
// is attacker-choosable within that account — so a verified notification
// must never settle another merchant's row. A mismatch is absorbed like the
// unmatchable-target case: Warn, no advance, ACK (the evidence row keeps
// the delivery for forensics).
func (s *intakeImpl) advance(ctx context.Context, notif *payentity.ChannelNotification) (bool, error) {
	switch notif.Kind {
	case payentity.NotifyKindPayment:
		targetNo, err := strconv.ParseInt(notif.TargetNo, 10, 64)
		if err != nil {
			s.logger.Warn("payment notification names an unmatchable target",
				slog.String("operation", "pay.svc.notify.advance"),
				slog.String("target_no", notif.TargetNo))
			return false, nil
		}
		p, err := s.payments.Get(ctx, targetNo)
		if err != nil {
			if errors.Is(err, pay.ErrPaymentNotFound) {
				s.logger.Warn("payment notification names an unmatchable target",
					slog.String("operation", "pay.svc.notify.advance"),
					slog.String("target_no", notif.TargetNo))
				return false, nil
			}
			return false, err
		}
		if p.MerchantID != notif.MerchantID || p.Channel != notif.Channel {
			s.logger.Warn("verified notification targets another merchant's payment",
				slog.String("operation", "pay.svc.notify.advance"),
				slog.Int64("notify_merchant_id", notif.MerchantID),
				slog.Int64("target_merchant_id", p.MerchantID),
				slog.Int64("target_payment_id", p.ID))
			return false, nil
		}
		return s.advancePayment(ctx, p, notif, targetNo)
	case payentity.NotifyKindRefund:
		targetNo, err := strconv.ParseInt(notif.TargetNo, 10, 64)
		if err != nil {
			s.logger.Warn("refund notification names an unmatchable target",
				slog.String("operation", "pay.svc.notify.advance"),
				slog.String("target_no", notif.TargetNo))
			return false, nil
		}
		rf, err := s.refunds.Get(ctx, targetNo)
		if err != nil {
			if errors.Is(err, pay.ErrRefundNotFound) {
				s.logger.Warn("refund notification names an unmatchable target",
					slog.String("operation", "pay.svc.notify.advance"),
					slog.String("target_no", notif.TargetNo))
				return false, nil
			}
			return false, err
		}
		if rf.MerchantID != notif.MerchantID || rf.Channel != notif.Channel {
			s.logger.Warn("verified notification targets another merchant's refund",
				slog.String("operation", "pay.svc.notify.advance"),
				slog.Int64("notify_merchant_id", notif.MerchantID),
				slog.Int64("target_merchant_id", rf.MerchantID),
				slog.Int64("target_refund_id", rf.ID))
			return false, nil
		}
		return s.refunds.ApplyRefundState(ctx, ApplyRefundStateRequest{
			RefundNo: targetNo,
			State: ChannelRefundState{
				State:           notif.RefundState,
				ChannelRefundNo: notif.ChannelTradeNo,
				RefundedAt:      notif.RefundedAt,
				RefundAmount:    notif.RefundAmount,
			},
		})
	default:
		s.logger.Warn("verified notification carries an unknown kind",
			slog.String("operation", "pay.svc.notify.advance"),
			slog.Int("kind", int(notif.Kind)))
		return false, nil
	}
}

// signHeaderSnapshot copies the verification-relevant request headers
// into the evidence row.
func signHeaderSnapshot(headers http.Header) map[string]string {
	snapshot := make(map[string]string, 6)
	for _, name := range []string{
		"Wechatpay-Timestamp", "Wechatpay-Nonce", "Wechatpay-Signature",
		"Wechatpay-Serial", "Wechatpay-Signature-Type",
	} {
		if v := headers.Get(name); v != "" {
			snapshot[name] = v
		}
	}
	if v := headers.Get("User-Agent"); v != "" {
		snapshot["User-Agent"] = v
	}
	return snapshot
}
