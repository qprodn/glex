// Package pay implements the pay module's application use cases: Merchant
// Account management, unified Payment initiation, Refunds, and Channel
// notification intake. Authentication and the operations' Functional
// Permission policy are enforced by the registered HTTP middleware.
package pay

import (
	"log/slog"

	payv1 "glex/api/pay/v1"
)

// PayApplication implements the pay management service. It owns the
// module's use cases; all row access goes through the pay domain services
// and all channel access through the gateway registry (ADR 0005).
type PayApplication struct {
	payv1.UnimplementedPayServiceServer

	logger *slog.Logger
}

// NewPayApplication creates the PayApplication.
func NewPayApplication(logger *slog.Logger) *PayApplication {
	return &PayApplication{
		logger: logger.With(slog.String("component", "application.pay")),
	}
}
