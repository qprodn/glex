package iam

import "context"

// Transaction runs fn inside a database transaction. The transaction is
// propagated through ctx: every repo call inside fn joins it, and nested InTx
// calls join the outermost transaction instead of starting a new one (join,
// not savepoint — an inner error rolls the whole transaction back). fn
// receives the tx-propagated ctx and must pass it to every repo call.
type Transaction interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
