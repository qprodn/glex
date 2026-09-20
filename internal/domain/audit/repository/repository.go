package repository

import (
	"context"

	"glex/internal/domain/audit/entity"
	"glex/pkg/listq"
)

// OperLogRepo is the persistence port for Oper Logs (ADR 0005): the audit
// domain service is its only consumer.
type OperLogRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.OperLog, error)
	List(ctx context.Context, q listq.Query) ([]*entity.OperLog, error)
	// Create inserts the record. Rows are write-once; no update exists.
	Create(ctx context.Context, o *entity.OperLog) (*entity.OperLog, error)
	// Delete withdraws the record (ADR 0002).
	Delete(ctx context.Context, id, actorID int64) error
}

// LoginLogRepo is the persistence port for Login Logs (ADR 0005).
type LoginLogRepo interface {
	List(ctx context.Context, q listq.Query) ([]*entity.LoginLog, error)
	// Create inserts the record. Rows are write-once; no update exists.
	Create(ctx context.Context, l *entity.LoginLog) (*entity.LoginLog, error)
	// Delete withdraws the record (ADR 0002).
	Delete(ctx context.Context, id, actorID int64) error
}
