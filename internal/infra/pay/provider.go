// Package payinfra hosts the pay module's persistence adapters: the four
// repos over ent, the credential envelope (ADR 0018), and — beside them —
// the channel adapter packages the GatewayRegistry resolves.
package payinfra

import (
	payservice "glex/internal/domain/pay/service"

	"go.uber.org/fx"
)

// Module contains the pay persistence adapters: the Merchant Account,
// Payment, Refund, and notify-log repos, the credential cipher, and the
// channel adapter registry with its two port bindings.
var Module = fx.Module("pay-infra",
	fx.Provide(
		NewCipher,
		NewAdapterRegistry,
		NewMerchantAccountRepo,
		NewPaymentRepo,
		NewRefundRepo,
		NewNotifyLogRepo,
		fx.Annotate(newCatalog, fx.As(new(payservice.ChannelCatalog))),
		fx.Annotate(newGatewayRegistry, fx.As(new(payservice.GatewayRegistry))),
		fx.Annotate(newConfigCipher, fx.As(new(payservice.ConfigCipher))),
	),
)
