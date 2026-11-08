package service

import (
	"context"
	"errors"
	"strings"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/logx"
)

func (s *appImpl) PublishMenu(ctx context.Context, req PublishMenuRequest) (*entity.App, error) {
	const op = "wechat.svc.app.publish_menu"
	current, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := menuAllowed(current); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := publishable(current); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if current.SavedMenu == nil {
		err = wechat.ErrMenuInvalid
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	call, err := s.publishCall(ctx, current)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if s.openapi == nil {
		err = wechat.ErrMenuRejected
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	result, err := s.openapi.PublishMenu(ctx, call)
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	switch result.Class {
	case CallDefinitiveRefusal:
		if err := s.lapse(ctx, current); err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		err = wechat.ErrAppLapsed
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	case CallSuccess:
	default:
		err = wechat.ErrMenuRejected
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if err := s.keepAuthorizerRefresh(ctx, current, result.AuthorizerRefreshToken); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	accepted := cloneMenu(*current.SavedMenu)
	current.AcceptedMenu = &accepted
	current.UpdateBy = req.ActorID
	updated, err := s.apps.Update(ctx, repository.SaveAppRequest{
		App:     current,
		ActorID: req.ActorID,
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return present(updated), nil
}

func (s *appImpl) publishCall(ctx context.Context, app *entity.App) (PublishMenuCall, error) {
	secrets, err := s.apps.LoadSecrets(ctx, app.ID)
	if err != nil {
		return PublishMenuCall{}, err
	}
	call := PublishMenuCall{
		Connection: app.Connection,
		AppID:      app.AppID,
		Menu:       cloneMenu(*app.SavedMenu),
	}
	if app.Connection == entity.ConnectionDirectHold {
		call.AppSecret = secrets.AppSecret
		return call, nil
	}
	material, blocked, err := s.platformProbeMaterial(ctx, app, secrets)
	if err != nil {
		return PublishMenuCall{}, err
	}
	if blocked != nil {
		return PublishMenuCall{}, wechat.ErrPlatformNotReady
	}
	call.ComponentAppID = material.ComponentAppID
	call.ComponentSecret = material.ComponentSecret
	call.VerifyTicket = material.VerifyTicket
	call.AuthorizerRefreshToken = material.AuthorizerRefreshToken
	return call, nil
}

// menuAllowed refuses a Custom Menu before WeChat is called. A Service
// Account may carry one. A Subscription Account or a mini program may not.
// A Platform Authorization must also have been granted the menu capability.
func menuAllowed(app *entity.App) error {
	if app == nil || app.AppType != entity.AppTypeOfficialAccount || app.Kind != entity.AccountKindService {
		return wechat.ErrCapabilityUnavailable
	}
	if app.Connection == entity.ConnectionPlatformAuthorization && !hasCapability(app.Capabilities, entity.CapabilityCustomMenu) {
		return wechat.ErrCapabilityUnavailable
	}
	return nil
}

func publishable(app *entity.App) error {
	if app.Suspended {
		return wechat.ErrAppSuspended
	}
	if app.Lapsed {
		return wechat.ErrAppLapsed
	}
	return nil
}

// validateMenu accepts a leaf that opens a web page or a mini program, and
// a parent that only holds sub-buttons. An empty document is not a menu.
func validateMenu(menu entity.CustomMenu) error {
	if len(menu.Buttons) == 0 {
		return wechat.ErrMenuInvalid
	}
	return validateButtons(menu.Buttons)
}

func cloneMenu(menu entity.CustomMenu) entity.CustomMenu {
	return entity.CustomMenu{Buttons: cloneButtons(menu.Buttons)}
}

func cloneButtons(buttons []entity.MenuButton) []entity.MenuButton {
	if len(buttons) == 0 {
		return nil
	}
	out := make([]entity.MenuButton, len(buttons))
	for i, button := range buttons {
		button.SubButtons = cloneButtons(button.SubButtons)
		out[i] = button
	}
	return out
}

func validateButtons(buttons []entity.MenuButton) error {
	for _, button := range buttons {
		if strings.TrimSpace(button.Name) == "" {
			return wechat.ErrMenuInvalid
		}
		web := strings.TrimSpace(button.WebURL) != ""
		miniID := strings.TrimSpace(button.MiniProgramAppID) != ""
		miniPage := strings.TrimSpace(button.MiniProgramPage) != ""
		mini := miniID || miniPage
		if len(button.SubButtons) > 0 {
			if web || mini {
				return wechat.ErrMenuInvalid
			}
			if err := validateButtons(button.SubButtons); err != nil {
				return err
			}
			continue
		}
		if web == mini {
			return wechat.ErrMenuInvalid
		}
		if mini && (!miniID || !miniPage) {
			return wechat.ErrMenuInvalid
		}
	}
	return nil
}
