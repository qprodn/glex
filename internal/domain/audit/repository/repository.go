package repository

import (
	"context"

	"glex/internal/domain/audit/entity"
	"glex/pkg/listq"
)

// DeleteRequest carries one withdrawal (ADR 0002): the row to withdraw and
// the actor performing it. Both repositories delete identically, so the two
// Delete methods share this one struct (ADR 0025).
type DeleteRequest struct {
	ID      int64
	ActorID int64
}

// OperLogRepo is the persistence port for Oper Logs (ADR 0005): the audit
// domain service is its only consumer.
type OperLogRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.OperLog, error)
	List(ctx context.Context, q listq.Query) ([]*entity.OperLog, error)
	// Create inserts the record. Rows are write-once; no update exists.
	Create(ctx context.Context, o *entity.OperLog) (*entity.OperLog, error)
	// Delete withdraws the record (ADR 0002).
	Delete(ctx context.Context, req DeleteRequest) error
}

// LoginLogRepo is the persistence port for Login Logs (ADR 0005).
type LoginLogRepo interface {
	List(ctx context.Context, q listq.Query) ([]*entity.LoginLog, error)
	// Create inserts the record. Rows are write-once; no update exists.
	Create(ctx context.Context, l *entity.LoginLog) (*entity.LoginLog, error)
	// Delete withdraws the record (ADR 0002).
	Delete(ctx context.Context, req DeleteRequest) error
}
