// Package impl adapts the pay module's capabilities to the crossdomain
// contracts: the creator shares the back office's application use case
// (the lock and transaction boundaries live there, design §8), and the
// reader adapts the payment domain service.
package impl

import "go.uber.org/fx"

// Module contains the pay crossdomain contract adapters.
var Module = fx.Module("pay-crossdomain",
	fx.Provide(
		NewPaymentCreator,
		NewPaymentReader,
	),
)
