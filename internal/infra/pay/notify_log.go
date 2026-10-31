package payinfra

import (
	"context"
	"log/slog"

	"glex/internal/domain/pay/entity"
	payrepo "glex/internal/domain/pay/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/paynotifylog"
	"glex/internal/infra/store"
)

type notifyLogRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewNotifyLogRepo creates the notify-log repo.
func NewNotifyLogRepo(data *store.Data) payrepo.NotifyLogRepo {
	return &notifyLogRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.pay.notify_log")),
	}
}

// storeError is this repo's log-and-map (ADR 0007). A unique-violation on
// the (channel, notify_id) index is not an unexpected failure — it is the
// dedup contract, mapped silently at the call site.
func (r *notifyLogRepo) storeError(ctx context.Context, operation string, err error) error {
	return payStoreError(ctx, r.logger, operation, err)
}

// Insert records one notification write-once. The (channel, notify_id)
// partial unique is the dedup: a constraint violation means the channel
// redelivered an already-accepted notification — reported as ok=false
// (an expected business condition, never logged as an error, ADR 0007).
func (r *notifyLogRepo) NextID() int64 {
	return newSnowflakeID()
}

func (r *notifyLogRepo) Insert(ctx context.Context, n *entity.ChannelNotification) (bool, error) {
	create := r.data.DB.PayNotifyLog(ctx).Create().
		SetID(n.ID).
		SetChannel(int16(n.Channel)).
		SetNotifyID(n.NotifyID).
		SetMerchantID(n.MerchantID).
		SetKind(int16(n.Kind)).
		SetTargetNo(n.TargetNo).
		SetVerified(n.Verified).
		SetProcessed(false).
		SetRawBody(n.RawBody)
	if len(n.SignHeaders) > 0 {
		create.SetSignHeaders(n.SignHeaders)
	}
	if _, err := create.Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			// The kept row is the evidence. Hand its id back so the
			// processed flag lands on that row, not on the id this
			// delivery minted and never inserted.
			existing, findErr := r.data.DB.PayNotifyLog(ctx).Query().
				Where(
					paynotifylog.ChannelEQ(int16(n.Channel)),
					paynotifylog.NotifyIDEQ(n.NotifyID),
					paynotifylog.DeletedAtIsNil(),
				).
				OnlyID(ctx)
			if findErr != nil {
				return false, r.storeError(ctx, "pay.notify_log.insert", findErr)
			}
			n.ID = existing
			return false, nil
		}
		return false, r.storeError(ctx, "pay.notify_log.insert", err)
	}
	return true, nil
}

// MarkProcessed flips the processed flag after the state advance ran. A
// lost race against a concurrent writer is irrelevant — the flag is
// diagnostic, the state advance's CAS is the guarantee.
func (r *notifyLogRepo) MarkProcessed(ctx context.Context, id int64) error {
	err := r.data.DB.PayNotifyLog(ctx).UpdateOneID(id).
		SetProcessed(true).
		Exec(ctx)
	if err != nil {
		return r.storeError(ctx, "pay.notify_log.mark_processed", err)
	}
	return nil
}
