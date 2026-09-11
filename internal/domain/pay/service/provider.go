package service

import "go.uber.org/fx"

// Module contains the pay domain service providers (ADR 0005). The
// payment and refund services join as their tickets land.
var Module = fx.Module("pay-domain-service",
	fx.Provide(
		fx.Annotate(NewMerchantService, fx.As(new(Merchant))),
		fx.Annotate(NewPaymentService, fx.As(new(Payment))),
		fx.Annotate(NewRefundService, fx.As(new(Refund))),
		fx.Annotate(NewNotificationService, fx.As(new(Notification))),
	),
)
