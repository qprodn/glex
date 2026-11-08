package repository

import (
	"context"

	"glex/internal/domain/wechat/entity"
)

// DispatchQuery selects a page of WeChat Dispatches. AppID 0 lists every
// attempt, including those whose app has been withdrawn.
type DispatchQuery struct {
	AppID  int64
	Limit  int
	Offset int
}

// MarkDeliveredRequest writes one delivery report onto the attempts that
// carry this msgid. The actor is the system. An unknown or blank msgid
// changes nothing and inserts nothing. The row is not withdrawn.
type MarkDeliveredRequest struct {
	MsgID   string
	ActorID int64
}

// DispatchRepo is the repository interface for WeChat Dispatches. Rows
// are not hard-deleted. Withdrawing the app does not withdraw them.
type DispatchRepo interface {
	Create(ctx context.Context, row *entity.Dispatch) (*entity.Dispatch, error)
	List(ctx context.Context, q DispatchQuery) ([]*entity.Dispatch, error)
	// MarkDelivered records a delivery report. A msgid that matches no
	// attempt is not an error.
	MarkDelivered(ctx context.Context, req MarkDeliveredRequest) error
	NextID() int64
}
