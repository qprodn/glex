package service

import (
	"context"
	"errors"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/logx"
)

// Probe runs the WeChat Connectivity Test for one live app. The port
// already classifies the call. This method does not read WeChat error
// codes and does not record a WeChat Dispatch.
func (s *appImpl) Probe(ctx context.Context, id int64) (*ProbeOutcome, error) {
	const op = "wechat.svc.app.probe"
	current, err := s.apps.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if current.Lapsed {
		err = wechat.ErrAppLapsed
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	req, blocked, err := s.probeRequest(ctx, current)
	if err != nil || blocked != nil {
		return blocked, err
	}
	if s.openapi == nil {
		return &ProbeOutcome{OK: false, Message: "微信不可用"}, nil
	}
	result, err := s.openapi.ProbeApp(ctx, req)
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return &ProbeOutcome{OK: false, Message: "微信不可用"}, nil
	}
	switch result.Class {
	case CallSuccess:
		if err := s.keepAuthorizerRefresh(ctx, current, result.AuthorizerRefreshToken); err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		return &ProbeOutcome{OK: true, Message: probeMessage(result, "连接成功")}, nil
	case CallDefinitiveRefusal:
		if err := s.lapse(ctx, current); err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		return &ProbeOutcome{OK: false, Message: probeMessage(result, "微信拒绝了授权或密钥")}, nil
	case CallUnavailable:
		return &ProbeOutcome{OK: false, Message: probeMessage(result, "微信不可用")}, nil
	default:
		return &ProbeOutcome{OK: false, Message: probeMessage(result, "微信没有受理这次调用")}, nil
	}
}

// probeRequest builds the port call. A non-nil outcome means the test
// already failed without calling WeChat and without lapsing the app:
// the platform is not ready, or the token material is missing.
func (s *appImpl) probeRequest(ctx context.Context, app *entity.App) (ProbeAppRequest, *ProbeOutcome, error) {
	const op = "wechat.svc.app.probe"
	secrets, err := s.apps.LoadSecrets(ctx, app.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return ProbeAppRequest{}, nil, err
	}
	if app.Connection == entity.ConnectionDirectHold {
		if secrets.AppSecret == "" {
			return ProbeAppRequest{}, &ProbeOutcome{OK: false, Message: "缺少直连密钥"}, nil
		}
		return ProbeAppRequest{
			Connection: app.Connection,
			AppID:      app.AppID,
			AppSecret:  secrets.AppSecret,
		}, nil, nil
	}
	material, blocked, err := s.platformProbeMaterial(ctx, app, secrets)
	if err != nil || blocked != nil {
		return ProbeAppRequest{}, blocked, err
	}
	return material, nil, nil
}

func (s *appImpl) platformProbeMaterial(ctx context.Context, app *entity.App, secrets repository.AppSecrets) (ProbeAppRequest, *ProbeOutcome, error) {
	const op = "wechat.svc.app.probe"
	if s.platforms == nil {
		return ProbeAppRequest{}, &ProbeOutcome{OK: false, Message: "平台未就绪"}, nil
	}
	p, err := s.platforms.FindByID(ctx, app.PlatformID)
	if err != nil {
		if errors.Is(err, wechat.ErrPlatformNotFound) {
			return ProbeAppRequest{}, &ProbeOutcome{OK: false, Message: "平台未就绪"}, nil
		}
		logx.Propagate(ctx, s.logger, op, err)
		return ProbeAppRequest{}, nil, err
	}
	if !p.Ready {
		return ProbeAppRequest{}, &ProbeOutcome{OK: false, Message: "平台未就绪"}, nil
	}
	platformSecrets, err := s.platforms.LoadSecrets(ctx, p.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return ProbeAppRequest{}, nil, err
	}
	ticket, err := s.platforms.LoadVerifyTicket(ctx, p.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return ProbeAppRequest{}, nil, err
	}
	if platformSecrets.ComponentSecret == "" || platformSecrets.Token == "" || platformSecrets.EncodingAESKey == "" || ticket == "" || secrets.AuthorizerRefreshToken == "" {
		return ProbeAppRequest{}, &ProbeOutcome{OK: false, Message: "平台未就绪"}, nil
	}
	return ProbeAppRequest{
		Connection:             app.Connection,
		AppID:                  app.AppID,
		ComponentAppID:         p.ComponentAppID,
		ComponentSecret:        platformSecrets.ComponentSecret,
		VerifyTicket:           ticket,
		AuthorizerRefreshToken: secrets.AuthorizerRefreshToken,
	}, nil, nil
}

// keepAuthorizerRefresh stores a refresh token WeChat rotated during a
// successful test. An empty value leaves the stored material, the
// suspension, the lapse, and the connection untouched.
func (s *appImpl) keepAuthorizerRefresh(ctx context.Context, app *entity.App, refresh string) error {
	if refresh == "" || app.Connection != entity.ConnectionPlatformAuthorization {
		return nil
	}
	stored, err := s.apps.LoadSecrets(ctx, app.ID)
	if err != nil {
		return err
	}
	if stored.AuthorizerRefreshToken == refresh {
		return nil
	}
	stored.AuthorizerRefreshToken = refresh
	app.UpdateBy = systemActorID
	_, err = s.apps.Update(ctx, repository.SaveAppRequest{
		App:            app,
		Secrets:        stored,
		ReplaceSecrets: true,
		ActorID:        systemActorID,
	})
	return err
}

// lapse records that WeChat definitively refused this same app. The
// stamp is the system itself: WeChat caused the write.
func (s *appImpl) lapse(ctx context.Context, app *entity.App) error {
	app.Lapsed = true
	app.UpdateBy = systemActorID
	_, err := s.apps.Update(ctx, repository.SaveAppRequest{
		App:     app,
		ActorID: systemActorID,
	})
	return err
}

func probeMessage(result ProbeAppResult, fallback string) string {
	if result.Message != "" {
		return result.Message
	}
	return fallback
}
