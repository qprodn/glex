package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/logx"
)

// StartAuthorization returns the link for one ready, non-withdrawn
// platform. An unready or withdrawn platform is refused. No WeChat App
// is created: the app appears only when the grant completes.
func (s *appImpl) StartAuthorization(ctx context.Context, req StartAuthorizationRequest) (*AuthorizationLink, error) {
	p, secrets, ticket, err := s.readyPlatform(ctx, req.PlatformID, "wechat.svc.app.start_authorization")
	if err != nil {
		return nil, err
	}
	if s.openapi == nil {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, "wechat.svc.app.start_authorization", err)
		return nil, err
	}
	link, err := s.openapi.StartAuthorization(ctx, OpenAuthorizationStart{
		ComponentAppID:  p.ComponentAppID,
		ComponentSecret: secrets.ComponentSecret,
		VerifyTicket:    ticket,
		RedirectURL:     authorizationReturnURL(s.callbackBase, p.ID),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.start_authorization", err)
		return nil, err
	}
	if link == nil || strings.TrimSpace(link.URL) == "" {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, "wechat.svc.app.start_authorization", err)
		return nil, err
	}
	return &AuthorizationLink{URL: link.URL}, nil
}

// CompleteAuthorization records the WeChat App the port reports for one
// completed grant. The row is stamped as the system itself (ADR 0002):
// WeChat caused the write, not an operator. An unready or withdrawn
// platform is refused and changes nothing.
func (s *appImpl) CompleteAuthorization(ctx context.Context, req CompleteAuthorizationRequest) (*entity.App, error) {
	const op = "wechat.svc.app.complete_authorization"
	if strings.TrimSpace(req.AuthCode) == "" {
		err := wechat.ErrAppInvalid
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	p, secrets, ticket, err := s.readyPlatform(ctx, req.PlatformID, op)
	if err != nil {
		return nil, err
	}
	if s.openapi == nil {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	grant, err := s.openapi.CompleteAuthorization(ctx, OpenAuthorizationCode{
		ComponentAppID:  p.ComponentAppID,
		ComponentSecret: secrets.ComponentSecret,
		VerifyTicket:    ticket,
		AuthCode:        req.AuthCode,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return s.applyGrant(ctx, p, grant)
}

// CountLiveAuthorizations reports how many apps not withdrawn are
// authorized through this platform. The platform service refuses
// withdrawal while the count is not zero.
func (s *appImpl) CountLiveAuthorizations(ctx context.Context, platformID int64) (int, error) {
	n, err := s.apps.CountLiveByPlatform(ctx, platformID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.svc.app.count_live_authorizations", err)
		return 0, err
	}
	return n, nil
}

func (s *appImpl) readyPlatform(ctx context.Context, platformID int64, op string) (*entity.Platform, repository.Secrets, string, error) {
	if s.platforms == nil {
		err := wechat.ErrPlatformNotFound
		logx.Propagate(ctx, s.logger, op, err)
		return nil, repository.Secrets{}, "", err
	}
	p, err := s.platforms.FindByID(ctx, platformID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, repository.Secrets{}, "", err
	}
	if !p.Ready {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, op, err)
		return nil, repository.Secrets{}, "", err
	}
	secrets, err := s.platforms.LoadSecrets(ctx, p.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, repository.Secrets{}, "", err
	}
	ticket, err := s.platforms.LoadVerifyTicket(ctx, p.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, repository.Secrets{}, "", err
	}
	if secrets.ComponentSecret == "" || secrets.Token == "" || secrets.EncodingAESKey == "" || ticket == "" {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, op, err)
		return nil, repository.Secrets{}, "", err
	}
	return p, secrets, ticket, nil
}

func (s *appImpl) applyGrant(ctx context.Context, p *entity.Platform, grant *AuthorizationGrant) (*entity.App, error) {
	const op = "wechat.svc.app.complete_authorization"
	if err := validateGrant(grant); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	existing, err := s.apps.FindLiveByAppID(ctx, grant.AppID)
	if err != nil && !errors.Is(err, wechat.ErrAppNotFound) {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	caps := normalizeCapabilities(grant.Capabilities)
	refresh := repository.AppSecrets{AuthorizerRefreshToken: grant.RefreshToken}
	if existing != nil {
		if existing.Connection != entity.ConnectionPlatformAuthorization || existing.PlatformID != p.ID {
			err = wechat.ErrAppDuplicate
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		existing.Capabilities = caps
		existing.Lapsed = false
		existing.UpdateBy = systemActorID
		updated, err := s.apps.Update(ctx, repository.SaveAppRequest{
			App:            existing,
			Secrets:        refresh,
			ReplaceSecrets: true,
			ActorID:        systemActorID,
		})
		if err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		return present(updated), nil
	}
	app := &entity.App{
		ID:           s.apps.NextID(),
		Name:         grant.Name,
		AppID:        grant.AppID,
		AppType:      grant.AppType,
		Kind:         grant.Kind,
		Connection:   entity.ConnectionPlatformAuthorization,
		PlatformID:   p.ID,
		Capabilities: caps,
		WebAuthScope: entity.WebAuthScopeOpenID,
		KeyVersion:   entity.KeyVersion,
		CreateBy:     systemActorID,
		UpdateBy:     systemActorID,
	}
	created, err := s.apps.Create(ctx, repository.SaveAppRequest{
		App:            app,
		Secrets:        refresh,
		ReplaceSecrets: true,
		ActorID:        systemActorID,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return present(created), nil
}

func validateGrant(g *AuthorizationGrant) error {
	if g == nil || validateAppName(g.Name) != nil || validateAppID(g.AppID) != nil {
		return wechat.ErrAppInvalid
	}
	if strings.TrimSpace(g.RefreshToken) == "" || len([]rune(g.RefreshToken)) > maxSecretLen {
		return wechat.ErrAppInvalid
	}
	switch g.AppType {
	case entity.AppTypeMiniProgram:
		if g.Kind != "" {
			return wechat.ErrAppInvalid
		}
		return nil
	case entity.AppTypeOfficialAccount:
		if g.Kind != entity.AccountKindService && g.Kind != entity.AccountKindSubscription {
			return wechat.ErrAppInvalid
		}
		return nil
	default:
		return wechat.ErrAppInvalid
	}
}

func normalizeCapabilities(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if !knownCapability(c) {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

func knownCapability(v string) bool {
	switch v {
	case entity.CapabilityCustomMenu,
		entity.CapabilityTemplateMessage,
		entity.CapabilitySubscribeNotification,
		entity.CapabilityWebAuthorization,
		entity.CapabilityMiniProgramLogin,
		entity.CapabilitySubscribeMessage:
		return true
	default:
		return false
	}
}

func authorizationReturnURL(base string, id int64) string {
	path := fmt.Sprintf("/wechat/platform/%d/authorization-return", id)
	if base == "" {
		return path
	}
	return base + path
}
