package wechat

import (
	"context"

	wechatv1 "glex/api/wechat/v1"
	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
	"glex/pkg/logx"
)

// GetWechatAppMenu returns the menu saved on the app and the last menu
// WeChat accepted. Audit fields are not part of this view.
func (s *WechatApplication) GetWechatAppMenu(ctx context.Context, req *wechatv1.GetWechatAppMenuRequest) (*wechatv1.WechatAppMenu, error) {
	app, err := s.apps.Get(ctx, req.GetAppId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.get_app_menu", err)
		return nil, err
	}
	return menuView(app), nil
}

// SaveWechatAppMenu stores the menu and does not call WeChat. create_by
// is already on the app; update_by is stamped from the Principal.
func (s *WechatApplication) SaveWechatAppMenu(ctx context.Context, req *wechatv1.SaveWechatAppMenuRequest) (*wechatv1.WechatAppMenu, error) {
	app, err := s.apps.SaveMenu(ctx, wechatservice.SaveMenuRequest{
		ID:      req.GetAppId(),
		Menu:    menuDomain(req.GetMenu()),
		ActorID: s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.save_app_menu", err)
		return nil, err
	}
	return menuView(app), nil
}

// PublishWechatAppMenu replaces the Service Account's default menu at
// WeChat with the menu already saved.
func (s *WechatApplication) PublishWechatAppMenu(ctx context.Context, req *wechatv1.PublishWechatAppMenuRequest) (*wechatv1.WechatAppMenu, error) {
	app, err := s.apps.PublishMenu(ctx, wechatservice.PublishMenuRequest{
		ID:      req.GetAppId(),
		ActorID: s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.publish_app_menu", err)
		return nil, err
	}
	return menuView(app), nil
}

func menuView(app *entity.App) *wechatv1.WechatAppMenu {
	if app == nil {
		return &wechatv1.WechatAppMenu{}
	}
	return &wechatv1.WechatAppMenu{
		Saved:    menuProto(app.SavedMenu),
		Accepted: menuProto(app.AcceptedMenu),
	}
}

func menuProto(menu *entity.CustomMenu) *wechatv1.WechatCustomMenu {
	if menu == nil {
		return &wechatv1.WechatCustomMenu{}
	}
	return &wechatv1.WechatCustomMenu{Buttons: buttonsProto(menu.Buttons)}
}

func buttonsProto(buttons []entity.MenuButton) []*wechatv1.WechatMenuButton {
	if len(buttons) == 0 {
		return nil
	}
	out := make([]*wechatv1.WechatMenuButton, 0, len(buttons))
	for _, button := range buttons {
		out = append(out, &wechatv1.WechatMenuButton{
			Name:             button.Name,
			WebUrl:           button.WebURL,
			MiniProgramAppId: button.MiniProgramAppID,
			MiniProgramPage:  button.MiniProgramPage,
			SubButtons:       buttonsProto(button.SubButtons),
		})
	}
	return out
}

func menuDomain(menu *wechatv1.WechatCustomMenu) entity.CustomMenu {
	if menu == nil {
		return entity.CustomMenu{}
	}
	return entity.CustomMenu{Buttons: buttonsDomain(menu.GetButtons())}
}

func buttonsDomain(buttons []*wechatv1.WechatMenuButton) []entity.MenuButton {
	if len(buttons) == 0 {
		return nil
	}
	out := make([]entity.MenuButton, 0, len(buttons))
	for _, button := range buttons {
		if button == nil {
			continue
		}
		out = append(out, entity.MenuButton{
			Name:             button.GetName(),
			WebURL:           button.GetWebUrl(),
			MiniProgramAppID: button.GetMiniProgramAppId(),
			MiniProgramPage:  button.GetMiniProgramPage(),
			SubButtons:       buttonsDomain(button.GetSubButtons()),
		})
	}
	return out
}
