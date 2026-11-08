// Package repository declares the wechat module's row ports. Infra
// implements them; the domain service is their only consumer (ADR 0005).
package repository

import (
	"context"

	"glex/internal/domain/wechat/entity"
	"glex/pkg/listq"
)

// Secrets is the operator-supplied secret triple of one WeChat Platform.
// It crosses the repository boundary only on a write or an explicit load
// during secret replacement. Display paths never receive it.
type Secrets struct {
	ComponentSecret string
	Token           string
	EncodingAESKey  string
}

// SavePlatformRequest is one insert or rewrite. Secrets is sealed by the
// repository when ReplaceSecrets is set; a name-only rewrite leaves the
// stored ciphertext untouched and does not require the master key.
type SavePlatformRequest struct {
	Platform       *entity.Platform
	Secrets        Secrets
	ReplaceSecrets bool
	ActorID        int64
}

// DeletePlatformRequest is one withdrawal (ADR 0002): the platform to
// soft-delete and the actor stamped into the audit fields.
type DeletePlatformRequest struct {
	ID      int64
	ActorID int64
}

// HoldVerifyTicketRequest stores the platform's current verify ticket as
// ciphertext. The plaintext never rides back on the platform the caller
// reads. ActorID is the system itself when WeChat pushed the ticket.
type HoldVerifyTicketRequest struct {
	ID      int64
	Ticket  string
	ActorID int64
}

// ReleaseVerifyTicketRequest records that the platform no longer holds a
// verify ticket. It does not withdraw the platform.
type ReleaseVerifyTicketRequest struct {
	ID      int64
	ActorID int64
}

// PlatformRepo is the repository interface for WeChat Platform rows.
// Writes encrypt Secrets whole (ADR 0018); reads return masks and never
// decrypt. LoadSecrets opens the ciphertext explicitly for a replacement
// that keeps a blank field's stored value.
type PlatformRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Platform, error)
	List(ctx context.Context, q listq.Query) ([]*entity.Platform, error)
	// FindLiveByComponentAppID resolves the platform that is not withdrawn
	// and holds this component AppID. A missing row is ErrPlatformNotFound.
	FindLiveByComponentAppID(ctx context.Context, componentAppID string) (*entity.Platform, error)
	LoadSecrets(ctx context.Context, id int64) (Secrets, error)
	// LoadVerifyTicket opens the held ticket. An empty string means the
	// platform does not hold one. The display path never calls this.
	LoadVerifyTicket(ctx context.Context, id int64) (string, error)
	Create(ctx context.Context, req SavePlatformRequest) (*entity.Platform, error)
	Update(ctx context.Context, req SavePlatformRequest) (*entity.Platform, error)
	// HoldVerifyTicket replaces the ticket this platform holds.
	HoldVerifyTicket(ctx context.Context, req HoldVerifyTicketRequest) (*entity.Platform, error)
	// ReleaseVerifyTicket clears the held ticket. A platform that held
	// none stays that way.
	ReleaseVerifyTicket(ctx context.Context, req ReleaseVerifyTicketRequest) (*entity.Platform, error)
	Delete(ctx context.Context, req DeletePlatformRequest) error
	NextID() int64
}
