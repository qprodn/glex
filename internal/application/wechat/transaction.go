package wechat

import (
	"context"
	"errors"

	wechat "glex/internal/domain/wechat"
	"glex/pkg/logx"
)

// Transaction is the wechat module's ambient-transaction port (ADR 0005).
// Adopt, publish-menu, and send each update the authorizer refresh and
// then write the template, the accepted menu, or the dispatch. Those two
// writes are one batch. Bound at the composition root to *store.TxManager.
type Transaction interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

// commitOutcome reports a business result whose writes must stay. A
// lapse and a rejected dispatch are recorded and then returned as
// errors. Rolling those back would drop the row the use case just wrote.
// Any other error fails the transaction, so a second-write failure does
// not leave the authorizer-refresh update committed.
func commitOutcome(err error) bool {
	return errors.Is(err, wechat.ErrAppLapsed) || errors.Is(err, wechat.ErrDispatchRejected)
}

func (s *WechatApplication) inTx(ctx context.Context, op string, fn func(context.Context) error) error {
	var outcome error
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		callErr := fn(ctx)
		if commitOutcome(callErr) {
			outcome = callErr
			return nil
		}
		return callErr
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return err
	}
	if outcome != nil {
		logx.Propagate(ctx, s.logger, op, outcome)
		return outcome
	}
	return nil
}
