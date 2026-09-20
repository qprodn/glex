package server

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"strconv"

	payapp "glex/internal/application/pay"
	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// OperationPayServiceChannelNotification names the hand-written notify
// route (ADR 0020: kratos proto generators cannot express a route keyed
// by channel + merchant path params with a non-JSON dialect body). It is
// deliberately absent from the auth route table — the channel's request
// signature is the authentication — and present in the operlog skip set:
// no Principal rides a channel push, the notify_log row itself is the
// record.
const OperationPayServiceChannelNotification = "/pay.v1.PayService/ChannelNotification"

// NotifyLimiter is the notify route group's rate-limit filter — the
// composition root binds the Redis fixed-window limiter into this
// net/http middleware shape.
type NotifyLimiter func(next stdhttp.Handler) stdhttp.Handler

// registerPayNotifyHTTP wires the channel notification route:
// POST /pay/notify/{channel}/{merchant_id} (ADR 0020).
func registerPayNotifyHTTP(s *khttp.Server, app *payapp.PayApplication, limiter NotifyLimiter) {
	filters := []khttp.FilterFunc{}
	if limiter != nil {
		filters = append(filters, khttp.FilterFunc(limiter))
	}
	r := s.Route("/", filters...)
	r.Handle("POST", "/pay/notify/{channel}/{merchant_id}", payNotifyHandler(app))
}

// payNotifyInvalid wraps a malformed notify request as the module's
// notify-invalid error (HTTP 400).
func payNotifyInvalid(cause error) error {
	if cause == nil {
		return pay.ErrNotifyInvalid
	}
	return pay.ErrNotifyInvalid.WithCause(cause)
}

// maxNotifyBodyBytes bounds one channel push before it is read: the route
// is deliberately public (the signature is the auth), nothing in the server
// stack caps request bytes, and the raw body is persisted verbatim — an
// uncapped read is a memory- and storage-exhaustion vector. WeChat v3 JSON
// and Alipay form bodies are a few KiB; 64 KiB leaves generous headroom.
const maxNotifyBodyBytes = 64 << 10

func payNotifyHandler(app *payapp.PayApplication) khttp.HandlerFunc {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, OperationPayServiceChannelNotification)
		// Validate the path before paying for the body at all.
		channel := payentity.ChannelByCode(ctx.Vars().Get("channel"))
		merchantID, parseErr := strconv.ParseInt(ctx.Vars().Get("merchant_id"), 10, 64)
		if parseErr != nil || !channel.Valid() {
			return payNotifyInvalid(parseErr)
		}
		body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, maxNotifyBodyBytes+1))
		if err != nil {
			return payNotifyInvalid(err)
		}
		if len(body) > maxNotifyBodyBytes {
			return payNotifyInvalid(errNotifyBodyTooLarge)
		}
		h := ctx.Middleware(func(requestCtx context.Context, _ any) (any, error) {
			return app.HandleChannelNotification(requestCtx, &payapp.ChannelNotificationRequest{
				Channel:    channel,
				MerchantID: merchantID,
				Headers:    ctx.Header(),
				Body:       body,
			})
		})
		out, err := h(ctx, &struct{}{})
		if err != nil {
			// Kratos' error encoder renders the kratos errors (404 for an
			// unknown merchant path, 400 for a failed verification) in its
			// standard JSON shape; the channels retry on anything that is
			// not their ACK, which the dedup absorbs.
			return err
		}
		// Durable processing done: answer in the channel's own dialect.
		ack := out.(*payapp.NotifyOutcome).AckBody
		ctx.Response().Header().Set("Content-Type", "application/json")
		ctx.Response().WriteHeader(stdhttp.StatusOK)
		_, _ = ctx.Response().Write([]byte(ack))
		return nil
	}
}

// errNotifyBodyTooLarge rejects an oversized delivery. A legitimate channel
// push never approaches the cap, so there is nothing to retry into — the
// 400 makes the channel give up on garbage traffic.
var errNotifyBodyTooLarge = errors.New("notify body exceeds the size cap")
