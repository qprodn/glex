package repository

import (
	"context"

	"glex/internal/domain/wechat/entity"
)

// FindByAppAndLibraryRequest selects the live adoption of one
// public-library template on one app.
type FindByAppAndLibraryRequest struct {
	AppID             int64
	LibraryTemplateID string
}

// TemplateRepo is the repository interface for Adopted Templates. Rows
// are not hard-deleted. Withdrawing the WeChat App does not withdraw
// the templates adopted onto it.

type TemplateRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.AdoptedTemplate, error)
	// FindByAppAndLibrary resolves the live adoption of one public-library
	// template on one app. A missing row is ErrTemplateNotFound.
	FindByAppAndLibrary(ctx context.Context, req FindByAppAndLibraryRequest) (*entity.AdoptedTemplate, error)
	ListByApp(ctx context.Context, appID int64) ([]*entity.AdoptedTemplate, error)
	Create(ctx context.Context, row *entity.AdoptedTemplate) (*entity.AdoptedTemplate, error)
	NextID() int64
}
