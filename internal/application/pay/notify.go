package pay

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/pkg/logx"
)

// NotifyOutcome is one notification intake's result. AckBody is the
// channel's dialect answer — the HTTP handler writes it verbatim after
// durable processing (ADR 0020).
type NotifyOutcome struct {
	// Processed is false for duplicate deliveries (the dedup absorbed the
	// redelivery) — still ACKed, nothing re-ran.
	Processed bool
	AckBody   string
}

// ChannelNotificationRequest is one channel delivery handed to the
// intake: the route's resolved channel and merchant plus the raw HTTP
// evidence (ADR 0025 — the delivery rides one request struct).
type ChannelNotificationRequest struct {
	Channel    payentity.Channel
	MerchantID int64
	Headers    http.Header
	Body       []byte
}

// HandleChannelNotification runs the intake pipeline: resolve the
// merchant from the path (404 when unknown — disabled or withdrawn
// merchants still resolve, their in-flight payments settle), verify the
// signature (verification IS the authentication), record the write-once
// evidence, advance the target by CAS, and return the dialect ACK.
// Verification failures record unverified evidence and return
// CHANNEL_NOTIFY_INVALID — the handler rejects, the channel retries, and
// the (channel, notify_id) dedup absorbs the loop.
func (s *PayApplication) HandleChannelNotification(
	ctx context.Context,
	req *ChannelNotificationRequest,
) (*NotifyOutcome, error) {
	channel, merchantID, headers, body := req.Channel, req.MerchantID, req.Headers, req.Body
	if !channel.Valid() {
		err := pay.ErrNotifyInvalid
		logx.Propagate(ctx, s.logger, "pay.notify.intake", err)
		return nil, err
	}
	resolved, err := s.merchants.ResolveGateway(ctx, merchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.notify.intake", err)
		return nil, err
	}
	notif, parseErr := resolved.Gateway.ParseNotification(ctx, payservice.NotificationRequest{
		Merchant: resolved.Merchant,
		Headers:  headers,
		Body:     body,
	})
	notif.MerchantID = merchantID
	notif.ID = s.notifications.NextID()
	if len(notif.SignHeaders) == 0 {
		notif.SignHeaders = signHeaderSnapshot(headers)
	}
	notif.RawBody = string(body)
	if parseErr != nil {
		// The evidence row for a rejected delivery: verified=false,
		// nothing advanced. A redelivered duplicate garbage payload just
		// loses the insert race — the rejection is unchanged.
		notif.Verified = false
		if _, insertErr := s.notifications.Record(ctx, &notif); insertErr != nil {
			s.logger.Warn("unverified notify evidence insert failed",
				slog.String("operation", "pay.notify.intake"),
				slog.Any("error", insertErr))
		}
		err = pay.ErrNotifyInvalid.WithCause(parseErr)
		logx.Propagate(ctx, s.logger, "pay.notify.intake", err)
		return nil, err
	}
	// The evidence insert is the pipeline's durability gate — without it
	// the state advance must not run; a non-ACK makes the channel retry
	// (ADR 0020). The dedup bool is deliberately unused below: the advance
	// re-runs for duplicates too (idempotent CAS), so the outcome reads
	// from the advance, not the insert.
	if _, err := s.notifications.Record(ctx, &notif); err != nil {
		logx.Propagate(ctx, s.logger, "pay.notify.intake", err)
		return nil, err
	}
	// The advance runs for new AND duplicate deliveries: every state
	// transition is an idempotent CAS, so a redelivery of an applied
	// notification is a no-op — while a redelivery whose first advance
	// FAILED gets healed instead of being ACKed into a lost update (the
	// dedup alone must never retire an unprocessed event).
	processed, err := s.advance(ctx, &notif)
	if err != nil {
		// The advance failed — no ACK, the channel retries, and the next
		// delivery (duplicate or not) re-attempts the advance.
		logx.Propagate(ctx, s.logger, "pay.notify.advance", err)
		return nil, err
	}
	if err := s.notifications.MarkProcessed(ctx, notif.ID); err != nil {
		// The flag is diagnostic; the CAS advance is the guarantee (the
		// handling site, ADR 0007).
		s.logger.Warn("notify processed-flag update failed",
			slog.String("operation", "pay.notify.intake"),
			slog.Int64("notify_log_id", notif.ID),
			slog.Any("error", err))
	}
	return &NotifyOutcome{Processed: processed, AckBody: ackBody(channel)}, nil
}

// advancePayment runs the payment advance and, when the transition into
// the paid state actually landed, announces Payment Completed on the pay
// topic (ADR 0022's tracer bullet).
func (s *PayApplication) advancePayment(
	ctx context.Context,
	p *payentity.Payment,
	notif *payentity.ChannelNotification,
	targetNo int64,
) (bool, error) {
	advanced, err := s.payments.ApplyChannelState(ctx, payservice.ApplyChannelStateRequest{
		PaymentNo: targetNo,
		State: payservice.ChannelPaymentState{
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

// publishPaymentCompleted announces the completed payment on the pay
// topic. Publishing is best-effort and never fails the notification
// intake: the state change is already durable and the channel must still
// be ACKed, so a lost publish is Warn-logged and accepted — the same
// posture as the audit streams (ADR 0012, 0022).
func (s *PayApplication) publishPaymentCompleted(ctx context.Context, p *payentity.Payment, notif *payentity.ChannelNotification) {
	event := payentity.NewPaymentCompletedEvent(s.notifications.NextID(), p, notif)
	body, err := json.Marshal(event)
	if err != nil {
		s.logger.Warn("payment completed event marshal failed",
			slog.String("operation", "pay.notify.publish_completed"),
			slog.Int64("payment_id", p.ID),
			slog.Any("error", err))
		return
	}
	if err := s.events.Send(ctx, body); err != nil {
		s.logger.Warn("payment completed publish degraded",
			slog.String("operation", "pay.notify.publish_completed"),
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
func (s *PayApplication) advance(ctx context.Context, notif *payentity.ChannelNotification) (bool, error) {
	switch notif.Kind {
	case payentity.NotifyKindPayment:
		targetNo, err := strconv.ParseInt(notif.TargetNo, 10, 64)
		if err != nil {
			// A verified event naming an unmatchable payment — nothing to
			// advance, retrying cannot fix it; the evidence row stands.
			s.logger.Warn("payment notification names an unmatchable target",
				slog.String("operation", "pay.notify.advance"),
				slog.String("target_no", notif.TargetNo))
			return false, nil
		}
		p, err := s.payments.Get(ctx, targetNo)
		if err != nil {
			if errors.Is(err, pay.ErrPaymentNotFound) {
				s.logger.Warn("payment notification names an unmatchable target",
					slog.String("operation", "pay.notify.advance"),
					slog.String("target_no", notif.TargetNo))
				return false, nil
			}
			logx.Propagate(ctx, s.logger, "pay.notify.advance", err)
			return false, err
		}
		if p.MerchantID != notif.MerchantID || p.Channel != notif.Channel {
			s.logger.Warn("verified notification targets another merchant's payment",
				slog.String("operation", "pay.notify.advance"),
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
				slog.String("operation", "pay.notify.advance"),
				slog.String("target_no", notif.TargetNo))
			return false, nil
		}
		rf, err := s.refunds.Get(ctx, targetNo)
		if err != nil {
			if errors.Is(err, pay.ErrRefundNotFound) {
				s.logger.Warn("refund notification names an unmatchable target",
					slog.String("operation", "pay.notify.advance"),
					slog.String("target_no", notif.TargetNo))
				return false, nil
			}
			logx.Propagate(ctx, s.logger, "pay.notify.advance", err)
			return false, err
		}
		if rf.MerchantID != notif.MerchantID || rf.Channel != notif.Channel {
			s.logger.Warn("verified notification targets another merchant's refund",
				slog.String("operation", "pay.notify.advance"),
				slog.Int64("notify_merchant_id", notif.MerchantID),
				slog.Int64("target_merchant_id", rf.MerchantID),
				slog.Int64("target_refund_id", rf.ID))
			return false, nil
		}
		return s.refunds.ApplyRefundState(ctx, payservice.ApplyRefundStateRequest{
			RefundNo: targetNo,
			State: payservice.ChannelRefundState{
				State:           notif.RefundState,
				ChannelRefundNo: notif.ChannelTradeNo,
				RefundedAt:      notif.RefundedAt,
				RefundAmount:    notif.RefundAmount,
			},
		})
	default:
		s.logger.Warn("verified notification carries an unknown kind",
			slog.String("operation", "pay.notify.advance"),
			slog.Int("kind", int(notif.Kind)))
		return false, nil
	}
}

// ackBody renders the channel's dialect answer (design §7): WeChat v3
// wants its JSON envelope, Alipay the literal "success". Anything else
// makes the channel retry.
func ackBody(channel payentity.Channel) string {
	switch channel {
	case payentity.ChannelWechat:
		return `{"code":"SUCCESS","message":"成功"}`
	default:
		return "success"
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
