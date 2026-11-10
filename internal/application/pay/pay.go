package pay

import (
	"context"
	"log/slog"

	payv1 "glex/api/pay/v1"
	"glex/internal/bizpkg"
	"glex/internal/conf"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/lock"
)

// PayApplication implements the pay management service. It owns the
// module's use cases; all row access goes through the pay domain services
// and all channel access through the gateway registry (ADR 0005).
type PayApplication struct {
	payv1.UnimplementedPayServiceServer

	merchants    payservice.Merchant
	applications payservice.Application
	payments     payservice.Payment
	refunds      payservice.Refund
	intake       payservice.Intake
	delivery     payservice.Delivery
	tx           Transaction
	locker       lock.Locker
	payConfig    *conf.Pay
	logger       *slog.Logger
}

// NewPayApplication creates the PayApplication.
func NewPayApplication(
	merchants payservice.Merchant,
	applications payservice.Application,
	payments payservice.Payment,
	refunds payservice.Refund,
	intake payservice.Intake,
	delivery payservice.Delivery,
	tx Transaction,
	locker lock.Locker,
	payConfig *conf.Pay,
	logger *slog.Logger,
) *PayApplication {
	return &PayApplication{
		merchants:    merchants,
		applications: applications,
		payments:     payments,
		refunds:      refunds,
		intake:       intake,
		delivery:     delivery,
		tx:           tx,
		locker:       locker,
		payConfig:    payConfig,
		logger:       logger.With(slog.String("component", "application.pay")),
	}
}

// actor reads the authenticated Principal from the request context (0 =
// the system itself, ADR 0002).
func (s *PayApplication) actor(ctx context.Context) int64 {
	return bizpkg.AuditActor(ctx)
}
