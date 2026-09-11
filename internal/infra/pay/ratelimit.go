package payinfra

import (
	"context"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"time"

	"glex/internal/conf"
	"glex/internal/infra/store"

	"github.com/redis/go-redis/v9"
)

// defaultNotifyRatePerMinute caps one merchant's notify deliveries when
// the config states nothing.
const defaultNotifyRatePerMinute = 60

// window is the fixed limiter window.
const window = time.Minute

// NotifyRateLimiter is the minimal Redis fixed-window limiter ADR 0020
// orders for exactly one route group: /pay/notify/{channel}/{merchant_id}.
// The merchant discriminator rides in the path, so the limit is per
// merchant — one channel's traffic cannot starve another's confirmations.
type NotifyRateLimiter struct {
	rdb       redis.UniversalClient
	perMinute int64
	logger    *slog.Logger
}

// NewNotifyRateLimiter creates the notify-route limiter.
func NewNotifyRateLimiter(data *store.Data, payConf *conf.Pay) *NotifyRateLimiter {
	perMinute := int64(defaultNotifyRatePerMinute)
	if v := payConf.GetNotifyRateLimitPerMinute(); v > 0 {
		perMinute = int64(v)
	}
	return &NotifyRateLimiter{
		rdb:       data.Redis,
		perMinute: perMinute,
		logger:    data.Logger.With(slog.String("component", "infra.pay.notify_limiter")),
	}
}

// Filter is the net/http middleware shape kratos Route groups accept.
func (l *NotifyRateLimiter) Filter(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if !l.allow(r.Context(), merchantIDOf(r.URL.Path)) {
			l.logger.WarnContext(r.Context(), "notify rate limited",
				slog.String("operation", "pay.notify.limiter"),
				slog.String("path", r.URL.Path))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(nethttp.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":"RATE_LIMITED","message":"too many notifications"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allow runs the fixed window: one INCR with an EXPIRE on the first hit
// of the window. A Redis failure fails open — a blip must never swallow
// payment confirmations; the Warn breadcrumb records it.
func (l *NotifyRateLimiter) allow(ctx context.Context, merchantID string) bool {
	if merchantID == "" {
		return false
	}
	key := fmt.Sprintf("glex:pay:notify:rl:%s:%d", merchantID, time.Now().Unix()/int64(window.Seconds()))
	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		l.logger.Warn("notify limiter unavailable; failing open",
			slog.String("operation", "pay.notify.limiter"),
			slog.Any("error", err))
		return true
	}
	return incr.Val() <= l.perMinute
}

// merchantIDOf extracts the trailing path segment — the notify route's
// merchant discriminator.
func merchantIDOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
