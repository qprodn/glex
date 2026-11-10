package pay

import (
	"context"
	"time"

	payservice "glex/internal/domain/pay/service"
)

// kickDelivery posts notices that are already waiting and due. It runs
// after the application method's state change has returned, outside the
// caller's transaction and outside channel-notification intake. The HTTP
// call is not awaited: a slow address must not block the channel ACK, and
// a missed kick stays due for the delivery job.
func (s *PayApplication) kickDelivery(ctx context.Context) {
	if s.delivery == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		// DeliverDue records attempt outcomes and logs a scan failure.
		// This kick must not fail the payment or the channel ACK.
		_, _ = s.delivery.DeliverDue(ctx, payservice.DeliverDueRequest{Now: time.Now()})
	}()
}
