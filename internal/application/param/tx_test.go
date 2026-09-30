package param

import (
	"context"
)

// inTxMarker is the value key fakeTransaction injects so tests can tell the
// transaction body's ctx apart from the caller's.
type inTxMarker struct{}

// fakeTransaction runs fn inline with a ctx derived from the caller's and
// returns the recorded body error, or — after a successful body — err,
// simulating a commit failure that happens after every service call inside
// the closure. bodyCtx records the ctx the body (and therefore the domain
// service inside it) observed, so tests can assert the handler passes the
// transaction ctx through instead of the caller's.
type fakeTransaction struct {
	err     error
	began   int
	bodyCtx context.Context
}

func (m *fakeTransaction) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	m.began++
	m.bodyCtx = context.WithValue(ctx, inTxMarker{}, true)
	if err := fn(m.bodyCtx); err != nil {
		return err
	}
	return m.err
}

func newFakeTransaction() *fakeTransaction {
	return &fakeTransaction{}
}
