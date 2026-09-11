package service

import "go.uber.org/fx"

// Module contains the pay domain service providers (ADR 0005). The
// payment and refund services join as their tickets land.
var Module = fx.Module("pay-domain-service",
	fx.Provide(
		fx.Annotate(NewMerchantService, fx.As(new(Merchant))),
	),
)
