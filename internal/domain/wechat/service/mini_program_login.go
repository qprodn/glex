package service

import (
	"context"
	"errors"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/pkg/logx"
)

// MiniProgramLoginRequest is one Mini Program Login: the mini program's
// AppID and the one-time login code. The caller does not say whether the
// app is a Direct Hold or a Platform Authorization, and the request
// carries no back-office Principal.
type MiniProgramLoginRequest struct {
	AppID string
	Code  string
}

// MiniProgramIdentity is the openid of one person under that WeChat App,
// plus the unionid when WeChat supplied one. It is not a WeChat User, a
// System User, a Login Session, or a system credential. session_key is
// absent: the Open API port drops it.
type MiniProgramIdentity struct {
	OpenID  string
	UnionID string
}

// MiniProgramLogin exchanges a one-time login code for an openid. Nothing
// is written: no person, no session, and no lapse on success.
func (s *appImpl) MiniProgramLogin(ctx context.Context, req MiniProgramLoginRequest) (*MiniProgramIdentity, error) {
	const op = "wechat.svc.app.mini_program_login"
	app, err := s.apps.FindLiveByAppID(ctx, req.AppID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err = usableMiniProgram(app); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	call, err := s.miniProgramLoginCall(ctx, app, req.Code)
	if err != nil {
		return nil, err
	}
	result, err := s.openapi.MiniProgramLogin(ctx, call)
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	switch result.Class {
	case CallDefinitiveRefusal:
		if err = s.lapse(ctx, app); err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		err = wechat.ErrWechatRefused
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	case CallSuccess:
		if result.OpenID == "" {
			err = wechat.ErrWechatRejected
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		return &MiniProgramIdentity{OpenID: result.OpenID, UnionID: result.UnionID}, nil
	default:
		err = wechat.ErrWechatRejected
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
}

// usableMiniProgram is the local gate. A failure here does not call
// WeChat, does not lapse the app, and stores no person.
func usableMiniProgram(app *entity.App) error {
	if app.Lapsed {
		return wechat.ErrAppLapsed
	}
	if app.Suspended {
		return wechat.ErrAppSuspended
	}
	if app.AppType != entity.AppTypeMiniProgram {
		return wechat.ErrAppInvalid
	}
	if app.Connection == entity.ConnectionPlatformAuthorization && !hasCapability(app.Capabilities, entity.CapabilityMiniProgramLogin) {
		return wechat.ErrAppCapability
	}
	return nil
}

func hasCapability(caps []string, name string) bool {
	for _, cap := range caps {
		if cap == name {
			return true
		}
	}
	return false
}

// miniProgramLoginCall builds the port exchange. The caller supplied only
// the AppID and the code; the connection decides which secret material
// goes with it. An unready platform fails here and does not lapse.
func (s *appImpl) miniProgramLoginCall(ctx context.Context, app *entity.App, code string) (MiniProgramLoginCall, error) {
	const op = "wechat.svc.app.mini_program_login"
	secrets, err := s.apps.LoadSecrets(ctx, app.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return MiniProgramLoginCall{}, err
	}
	if app.Connection == entity.ConnectionDirectHold {
		return MiniProgramLoginCall{
			ProbeAppRequest: ProbeAppRequest{
				Connection: app.Connection,
				AppID:      app.AppID,
				AppSecret:  secrets.AppSecret,
			},
			Code: code,
		}, nil
	}
	if s.platforms == nil {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, op, err)
		return MiniProgramLoginCall{}, err
	}
	p, err := s.platforms.FindByID(ctx, app.PlatformID)
	if err != nil {
		if errors.Is(err, wechat.ErrPlatformNotFound) {
			err = wechat.ErrPlatformNotReady
		}
		logx.Propagate(ctx, s.logger, op, err)
		return MiniProgramLoginCall{}, err
	}
	if !p.Ready {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, op, err)
		return MiniProgramLoginCall{}, err
	}
	platformSecrets, err := s.platforms.LoadSecrets(ctx, p.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return MiniProgramLoginCall{}, err
	}
	ticket, err := s.platforms.LoadVerifyTicket(ctx, p.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return MiniProgramLoginCall{}, err
	}
	if platformSecrets.ComponentSecret == "" || platformSecrets.Token == "" || platformSecrets.EncodingAESKey == "" || ticket == "" || secrets.AuthorizerRefreshToken == "" {
		err = wechat.ErrPlatformNotReady
		logx.Propagate(ctx, s.logger, op, err)
		return MiniProgramLoginCall{}, err
	}
	return MiniProgramLoginCall{
		ProbeAppRequest: ProbeAppRequest{
			Connection:             app.Connection,
			AppID:                  app.AppID,
			ComponentAppID:         p.ComponentAppID,
			ComponentSecret:        platformSecrets.ComponentSecret,
			VerifyTicket:           ticket,
			AuthorizerRefreshToken: secrets.AuthorizerRefreshToken,
		},
		Code: code,
	}, nil
}
