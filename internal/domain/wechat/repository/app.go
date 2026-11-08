package repository

import (
	"context"

	"glex/internal/domain/wechat/entity"
	"glex/pkg/listq"
)

// AppSecrets is the secret set of one Direct Hold: the operator-supplied
// AppSecret and the generated Push Secret. It crosses the repository
// boundary only on a write or an explicit load. Display paths never
// receive it.
type AppSecrets struct {
	AppSecret      string
	Token          string
	EncodingAESKey string
}

// SaveAppRequest is one insert or rewrite. Secrets is sealed by the
// repository when ReplaceSecrets is set. A name change or a suspension
// leaves the stored ciphertext untouched and does not require the master
// key.
type SaveAppRequest struct {
	App            *entity.App
	Secrets        AppSecrets
	ReplaceSecrets bool
	ActorID        int64
}

// DeleteAppRequest is one withdrawal (ADR 0002): the app to soft-delete
// and the actor stamped into the audit fields. The row stays; deleted_at
// is set.
type DeleteAppRequest struct {
	ID      int64
	ActorID int64
}

// AppRepo is the repository interface for WeChat App rows. Writes encrypt
// AppSecrets whole (ADR 0018); reads return masks and never decrypt.
// LoadSecrets opens the ciphertext explicitly when a replacement must keep
// a secret the caller did not resubmit. Among rows that are not withdrawn,
// one AppID identifies one app — a different rule from the platform's
// component AppID.
type AppRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.App, error)
	List(ctx context.Context, q listq.Query) ([]*entity.App, error)
	// FindLiveByAppID resolves the app that is not withdrawn and holds
	// this AppID. A missing row is ErrAppNotFound.
	FindLiveByAppID(ctx context.Context, appID string) (*entity.App, error)
	LoadSecrets(ctx context.Context, id int64) (AppSecrets, error)
	Create(ctx context.Context, req SaveAppRequest) (*entity.App, error)
	Update(ctx context.Context, req SaveAppRequest) (*entity.App, error)
	Delete(ctx context.Context, req DeleteAppRequest) error
	NextID() int64
}
