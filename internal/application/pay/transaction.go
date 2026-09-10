package pay

import (
	"context"
)

// Transaction is the pay module's ambient-transaction port (ADR 0005):
// CreatePayment's close-old-pending + insert runs as one indivisible
// batch. Bound at the composition root to *store.TxManager.
type Transaction interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}
