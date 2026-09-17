package payinfra

import (
	"context"
	"fmt"

	"glex/internal/domain/job"
	payservice "glex/internal/domain/pay/service"
)

// ExpireSweepHandler names the expire-sweep executor (ADR 0023's anchor
// use case).
const ExpireSweepHandler = "pay.expire-sweep"

// expireSweepBatch bounds one sweep run's channel calls: settle makes a
// Query (and possibly a Close) per candidate, so the batch caps a single
// run's channel traffic; the next minute's run resumes from the oldest.
const expireSweepBatch = 50

// expireSweepExecutor converges expired pending payments: the proactive
// half of expiry beside pay's lazy settlement paths. It holds no state —
// every firing re-derives its candidates from the payment rows, so a
// duplicate or missed firing is harmless by construction.
type expireSweepExecutor struct {
	payments payservice.Payment
}

// NewExpireSweepExecutor provides the pay module's job executor into the
// "job-executors" value group (ADR 0023) — the same wiring shape as the
// module's eventbus subscription.
func NewExpireSweepExecutor(payments payservice.Payment) job.Executor {
	return &expireSweepExecutor{payments: payments}
}

// Name implements job.Executor.
func (e *expireSweepExecutor) Name() string { return ExpireSweepHandler }

// Run implements job.Executor. The summary lands on the run's Job Log row.
func (e *expireSweepExecutor) Run(ctx context.Context, _ string) (string, error) {
	settled, err := e.payments.SweepExpired(ctx, expireSweepBatch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("settled %d expired pending payment(s)", settled), nil
}
