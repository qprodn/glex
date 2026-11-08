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

// DispatchRepo is the repository interface for WeChat Dispatches. Rows
// are not hard-deleted. Withdrawing the app does not withdraw them.
type DispatchRepo interface {
	Create(ctx context.Context, row *entity.Dispatch) (*entity.Dispatch, error)
	List(ctx context.Context, q DispatchQuery) ([]*entity.Dispatch, error)
	NextID() int64
}
