package store

import (
	"glex/internal/domain/iam"
)

// NewTxManager exposes the transaction-aware Database wrapper as the domain
// Transaction seam: InTx propagates the transaction through ctx, and nested
// calls join the outermost one.
func NewTxManager(d *Data) iam.Transaction {
	return d.DB
}
