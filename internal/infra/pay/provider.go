// Package payinfra hosts the pay module's persistence adapters: the four
// repos over ent, the credential envelope (ADR 0018), and — beside them —
// the channel adapter packages the GatewayRegistry resolves.
package payinfra

import (
	"go.uber.org/fx"
)

// Module contains the pay persistence adapters: the Merchant Account,
// Payment, Refund, and notify-log repos plus the credential cipher.
var Module = fx.Module("pay-infra",
	fx.Provide(
		NewCipher,
		NewMerchantAccountRepo,
		NewPaymentRepo,
		NewRefundRepo,
		NewNotifyLogRepo,
	),
)
