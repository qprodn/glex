package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// CallbackBase is the absolute origin WeChat calls, without a path and
// without a trailing slash requirement. Empty leaves each platform showing
// its path only. The value is deployment configuration, not a secret.
type CallbackBase string

// systemActorID stamps updates WeChat causes, and the record that a
// verify ticket is no longer held. 0 is the system itself (ADR 0002).
const systemActorID int64 = 0

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
	// Receive accepts one encrypted push for this platform. A verify
	// ticket makes that platform ready. Anything that is not an encrypted
	// push is rejected and changes nothing.
	Receive(ctx context.Context, req PushRequest) (*PushOutcome, error)
	// Probe runs the WeChat Connectivity Test. A platform that is missing
	// a secret or a verify ticket fails the test. Success and failure
	// leave the platform as it was and create no WeChat App.
	Probe(ctx context.Context, id int64) (*ProbeOutcome, error)
	// ReleaseVerifyTicket records that this platform no longer holds a
	// verify ticket, so it is unready. It is not an operator action and
	// not a back-office button: no WeChat App is created or lapsed.
	ReleaseVerifyTicket(ctx context.Context, id int64) (*entity.Platform, error)
}

type platformImpl struct {
	platforms    repository.PlatformRepo
	openapi      OpenAPI
	pushes       PushAuthenticator
	bindings     PlatformBindings
	callbackBase string
	logger       *slog.Logger
}

// NewPlatformService creates the platform domain service. openapi is the
// scripted boundary in tests and the HTTP adapter in the process. pushes
// is the real encrypted-push authenticator — plaintext, compatible mode,
// and a bad signature never reach platform state. bindings applies a
// grant WeChat pushed and refuses withdrawal while a live app remains.
func NewPlatformService(platforms repository.PlatformRepo, openapi OpenAPI, pushes PushAuthenticator, callbackBase CallbackBase, bindings PlatformBindings, logger *slog.Logger) Platform {
	return &platformImpl{
		platforms:    platforms,
		openapi:      openapi,
		pushes:       pushes,
		bindings:     bindings,
		callbackBase: strings.TrimRight(string(callbackBase), "/"),
		logger:       logger.With(slog.String("component", "domain.wechat.platform.svc")),
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
	return s.present(created), nil
}

func (s *platformImpl) Get(ctx context.Context, id int64) (*entity.Platform, error) {
	p, err := s.platforms.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.get", err)
		return nil, err
	}
	return s.present(p), nil
}

func (s *platformImpl) List(ctx context.Context, q listq.Query) ([]*entity.Platform, error) {
	platforms, err := s.platforms.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.list", err)
		return nil, err
	}
	for i, p := range platforms {
		platforms[i] = s.present(p)
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
	return s.present(updated), nil
}

func (s *platformImpl) Withdraw(ctx context.Context, req WithdrawRequest) error {
	if s.bindings != nil {
		n, err := s.bindings.CountLiveAuthorizations(ctx, req.ID)
		if err != nil {
			logx.Propagate(ctx, s.logger, "wechat.svc.platform.withdraw", err)
			return err
		}
		if n > 0 {
			err = wechat.ErrPlatformInUse
			logx.Propagate(ctx, s.logger, "wechat.svc.platform.withdraw", err)
			return err
		}
	}
	if err := s.platforms.Delete(ctx, repository.DeletePlatformRequest{
		ID:      req.ID,
		ActorID: req.ActorID,
	}); err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.platform.withdraw", err)
		return err
	}
	return nil
}

// present attaches the operator-visible callback address. Readiness is
// already decided by the repository from stored secrets and the held ticket.
func (s *platformImpl) present(p *entity.Platform) *entity.Platform {
	if p == nil {
		return nil
	}
	p.CallbackURL = platformCallbackURL(s.callbackBase, p.ID)
	return p
}

func platformCallbackURL(base string, id int64) string {
	path := fmt.Sprintf("/wechat/platform/%d/callback", id)
	if base == "" {
		return path
	}
	return base + path
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
