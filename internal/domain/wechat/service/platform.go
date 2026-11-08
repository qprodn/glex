package service

import (
	"context"
	"errors"
	"log/slog"
	"unicode/utf8"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// CreateRequest is what a platform-register use case states: the display
// name, the component AppID, the three secrets the operator typed, and the
// actor stamped into the audit fields.
type CreateRequest struct {
	Name            string
	ComponentAppID  string
	ComponentSecret string
	Token           string
	EncodingAESKey  string
	ActorID         int64
}

// UpdateRequest is one correction of a live platform. A non-empty
// ComponentAppID that differs from the stored value is refused — the
// component AppID is immutable (ADR 0029). A blank secret keeps its stored
// value; a name-only change does not touch the ciphertext.
type UpdateRequest struct {
	ID              int64
	Name            string
	ComponentAppID  string
	ComponentSecret string
	Token           string
	EncodingAESKey  string
	ActorID         int64
}

// WithdrawRequest is one platform withdrawal: the row and the actor
// stamped into the audit fields.
type WithdrawRequest struct {
	ID      int64
	ActorID int64
}

// Platform is the wechat platform domain service: the only path to WeChat
// Platform rows (ADR 0005). Ordinary results carry masks, never the
// secrets the operator typed (ADR 0029).
type Platform interface {
	Create(ctx context.Context, req CreateRequest) (*entity.Platform, error)
	Get(ctx context.Context, id int64) (*entity.Platform, error)
	List(ctx context.Context, q listq.Query) ([]*entity.Platform, error)
	Update(ctx context.Context, req UpdateRequest) (*entity.Platform, error)
	Withdraw(ctx context.Context, req WithdrawRequest) error
}

type platformImpl struct {
	platforms repository.PlatformRepo
	logger    *slog.Logger
}

// NewPlatformService creates the platform domain service.
func NewPlatformService(platforms repository.PlatformRepo, logger *slog.Logger) Platform {
	return &platformImpl{
		platforms: platforms,
		logger:    logger.With(slog.String("component", "domain.wechat.platform.svc")),
	}
}

const (
	maxNameLen   = 64
	maxAppIDLen  = 64
	maxSecretLen = 256
)

func (s *platformImpl) Create(ctx context.Context, req CreateRequest) (*entity.Platform, error) {
	if err := validateName(req.Name); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.create", err)
		return nil, err
	}
	if err := validateComponentAppID(req.ComponentAppID); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.create", err)
		return nil, err
	}
	if err := validateSecret(req.ComponentSecret, true); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.create", err)
		return nil, err
	}
	if err := validateSecret(req.Token, true); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.create", err)
		return nil, err
	}
	if err := validateSecret(req.EncodingAESKey, true); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.create", err)
		return nil, err
	}
	if err := s.ensureComponentAppIDAvailable(ctx, req.ComponentAppID); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.create", err)
		return nil, err
	}
	p := &entity.Platform{
		ID:                  s.platforms.NextID(),
		Name:                req.Name,
		ComponentAppID:      req.ComponentAppID,
		ComponentSecretMask: maskSecret(req.ComponentSecret),
		TokenMask:           maskSecret(req.Token),
		EncodingAESKeyMask:  maskSecret(req.EncodingAESKey),
		KeyVersion:          entity.KeyVersion,
		CreateBy:            req.ActorID,
		UpdateBy:            req.ActorID,
	}
	created, err := s.platforms.Create(ctx, repository.SavePlatformRequest{
		Platform: p,
		Secrets: repository.Secrets{
			ComponentSecret: req.ComponentSecret,
			Token:           req.Token,
			EncodingAESKey:  req.EncodingAESKey,
		},
		ReplaceSecrets: true,
		ActorID:        req.ActorID,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.create", err)
		return nil, err
	}
	return created, nil
}

func (s *platformImpl) Get(ctx context.Context, id int64) (*entity.Platform, error) {
	p, err := s.platforms.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.get", err)
		return nil, err
	}
	return p, nil
}

func (s *platformImpl) List(ctx context.Context, q listq.Query) ([]*entity.Platform, error) {
	platforms, err := s.platforms.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.list", err)
		return nil, err
	}
	return platforms, nil
}

func (s *platformImpl) Update(ctx context.Context, req UpdateRequest) (*entity.Platform, error) {
	if err := validateName(req.Name); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.update", err)
		return nil, err
	}
	current, err := s.platforms.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.update", err)
		return nil, err
	}
	if req.ComponentAppID != "" && req.ComponentAppID != current.ComponentAppID {
		err = wechat.ErrPlatformAppIDImmutable
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.update", err)
		return nil, err
	}
	save := repository.SavePlatformRequest{Platform: current, ActorID: req.ActorID}
	if req.ComponentSecret != "" || req.Token != "" || req.EncodingAESKey != "" {
		if err := validateSecret(req.ComponentSecret, false); err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.platform.update", err)
			return nil, err
		}
		if err := validateSecret(req.Token, false); err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.platform.update", err)
			return nil, err
		}
		if err := validateSecret(req.EncodingAESKey, false); err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.platform.update", err)
			return nil, err
		}
		stored, err := s.platforms.LoadSecrets(ctx, req.ID)
		if err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.platform.update", err)
			return nil, err
		}
		if req.ComponentSecret != "" {
			stored.ComponentSecret = req.ComponentSecret
		}
		if req.Token != "" {
			stored.Token = req.Token
		}
		if req.EncodingAESKey != "" {
			stored.EncodingAESKey = req.EncodingAESKey
		}
		current.ComponentSecretMask = maskSecret(stored.ComponentSecret)
		current.TokenMask = maskSecret(stored.Token)
		current.EncodingAESKeyMask = maskSecret(stored.EncodingAESKey)
		current.KeyVersion = entity.KeyVersion
		save.Secrets = stored
		save.ReplaceSecrets = true
	}
	current.Name = req.Name
	current.UpdateBy = req.ActorID
	updated, err := s.platforms.Update(ctx, save)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.update", err)
		return nil, err
	}
	return updated, nil
}

func (s *platformImpl) Withdraw(ctx context.Context, req WithdrawRequest) error {
	if err := s.platforms.Delete(ctx, repository.DeletePlatformRequest{
		ID:      req.ID,
		ActorID: req.ActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.withdraw", err)
		return err
	}
	return nil
}

func (s *platformImpl) ensureComponentAppIDAvailable(ctx context.Context, componentAppID string) error {
	existing, err := s.platforms.FindLiveByComponentAppID(ctx, componentAppID)
	if err != nil {
		if errors.Is(err, wechat.ErrPlatformNotFound) {
			return nil
		}
		return err
	}
	if existing != nil {
		return wechat.ErrPlatformDuplicate
	}
	return nil
}

func validateName(name string) error {
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return wechat.ErrPlatformInvalid
	}
	return nil
}

func validateComponentAppID(appID string) error {
	if appID == "" || utf8.RuneCountInString(appID) > maxAppIDLen {
		return wechat.ErrPlatformInvalid
	}
	return nil
}

func validateSecret(v string, required bool) error {
	if v == "" {
		if required {
			return wechat.ErrPlatformInvalid
		}
		return nil
	}
	if utf8.RuneCountInString(v) > maxSecretLen {
		return wechat.ErrPlatformInvalid
	}
	return nil
}

// maskSecret keeps the tail visible and hides the rest: `****ab12` for
// values long enough, a bare **** otherwise — the same shape merchant
// credentials return (ADR 0018).
func maskSecret(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}
