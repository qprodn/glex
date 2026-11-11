package service

import "context"

// Transaction is the ambient unit of work a money-moving compare-and-swap
// shares with its Business Notification insert. Bound at the composition
// root to *store.TxManager. Channel HTTP stays outside it: a process death
// before the first delivery attempt still leaves the due row committed
// with the money change.
type Transaction interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}
