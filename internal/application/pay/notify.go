package pay

import (
	"context"
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
	channel payentity.Channel,
	merchantID int64,
	headers http.Header,
	body []byte,
) (*NotifyOutcome, error) {
	if !channel.Valid() {
		err := pay.ErrNotifyInvalid
		logx.Propagate(ctx, s.logger, "pay.notify.intake", err)
		return nil, err
	}
	m, gw, err := s.merchants.ResolveGateway(ctx, merchantID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.notify.intake", err)
		return nil, err
	}
	notif, parseErr := gw.ParseNotification(ctx, m, headers, body)
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
	ok, err := s.notifications.Record(ctx, &notif)
	if err != nil {
		// The evidence insert is the pipeline's durability gate — without
		// it the state advance must not run; a non-ACK makes the channel
		// retry (ADR 0020).
		logx.Propagate(ctx, s.logger, "pay.notify.intake", err)
		return nil, err
	}
	if !ok {
		// Duplicate delivery: ACK without re-processing (design §7).
		return &NotifyOutcome{Processed: false, AckBody: ackBody(channel)}, nil
	}
	processed, err := s.advance(ctx, &notif)
	if err != nil {
		// The advance failed — no ACK, the channel retries, the dedup
		// absorbs its redelivery.
		logx.Propagate(ctx, s.logger, "pay.notify.advance", err)
		return nil, err
	}
	_ = processed
	if err := s.notifications.MarkProcessed(ctx, notif.ID); err != nil {
		// The flag is diagnostic; the CAS advance is the guarantee (the
		// handling site, ADR 0007).
		s.logger.Warn("notify processed-flag update failed",
			slog.String("operation", "pay.notify.intake"),
			slog.Int64("notify_log_id", notif.ID),
			slog.Any("error", err))
	}
	return &NotifyOutcome{Processed: true, AckBody: ackBody(channel)}, nil
}

// advance applies the verified event to its target row.
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
		return s.payments.ApplyChannelState(ctx, targetNo, payservice.ChannelPaymentState{
			State:          notif.PaymentState,
			ChannelTradeNo: notif.ChannelTradeNo,
			PayerID:        notif.PayerID,
			PaidAt:         notif.PaidAt,
		})
	case payentity.NotifyKindRefund:
		targetNo, err := strconv.ParseInt(notif.TargetNo, 10, 64)
		if err != nil {
			s.logger.Warn("refund notification names an unmatchable target",
				slog.String("operation", "pay.notify.advance"),
				slog.String("target_no", notif.TargetNo))
			return false, nil
		}
		return s.refunds.ApplyRefundState(ctx, targetNo, payservice.ChannelRefundState{
			State:           notif.RefundState,
			ChannelRefundNo: notif.ChannelTradeNo,
			RefundedAt:      notif.RefundedAt,
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
