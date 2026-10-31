package pay

import (
	"context"

	payservice "glex/internal/domain/pay/service"
	"glex/pkg/logx"
)

// HandleChannelNotification forwards one Channel Notification to the pay
// domain intake (ADR 0027). The HTTP handler is the only caller.
func (s *PayApplication) HandleChannelNotification(
	ctx context.Context,
	req *payservice.ChannelNotificationRequest,
) (*payservice.ChannelNotificationResult, error) {
	out, err := s.intake.Accept(ctx, req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "pay.notify.intake", err)
		return nil, err
	}
	return out, nil
}
