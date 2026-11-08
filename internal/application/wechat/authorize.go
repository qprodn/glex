package wechat

import (
	"context"

	wechatv1 "glex/api/wechat/v1"
	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// ListAuthorizationPlatforms lists ready platforms the operator can choose
// when starting a Platform Authorization. Withdrawn rows are already omitted
// by the domain list. Secrets and masks are not copied onto the choice.
func (s *WechatApplication) ListAuthorizationPlatforms(ctx context.Context, _ *wechatv1.ListAuthorizationPlatformsRequest) (*wechatv1.WechatAuthorizationPlatformSet, error) {
	q, _, err := listq.Parse(&wechatv1.ListWechatPlatformsRequest{PageSize: listq.MaxPageSize}, platformListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_authorization_platforms", err)
		return nil, err
	}
	platforms, err := s.platforms.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.list_authorization_platforms", err)
		return nil, err
	}
	set := &wechatv1.WechatAuthorizationPlatformSet{}
	for _, p := range platforms {
		if p == nil || !p.Ready {
			continue
		}
		set.Platforms = append(set.Platforms, &wechatv1.WechatAuthorizationPlatform{
			PlatformId:     p.ID,
			Name:           p.Name,
			ComponentAppId: p.ComponentAppID,
		})
	}
	return set, nil
}

// StartWechatAuthorization returns the link for one ready platform. The
// call creates no WeChat App. The grant WeChat later pushes is stamped as
// the system itself inside the domain service.
func (s *WechatApplication) StartWechatAuthorization(ctx context.Context, req *wechatv1.StartWechatAuthorizationRequest) (*wechatv1.WechatAuthorization, error) {
	link, err := s.apps.StartAuthorization(ctx, wechatservice.StartAuthorizationRequest{
		PlatformID: req.GetPlatformId(),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.start_authorization", err)
		return nil, err
	}
	return &wechatv1.WechatAuthorization{
		PlatformId: req.GetPlatformId(),
		Url:        link.URL,
	}, nil
}

func capabilitiesProto(in []string) []wechatv1.WechatCapability {
	out := make([]wechatv1.WechatCapability, 0, len(in))
	for _, name := range in {
		switch name {
		case entity.CapabilityCustomMenu:
			out = append(out, wechatv1.WechatCapability_WECHAT_CAPABILITY_CUSTOM_MENU)
		case entity.CapabilityTemplateMessage:
			out = append(out, wechatv1.WechatCapability_WECHAT_CAPABILITY_TEMPLATE_MESSAGE)
		case entity.CapabilitySubscribeNotification:
			out = append(out, wechatv1.WechatCapability_WECHAT_CAPABILITY_SUBSCRIBE_NOTIFICATION)
		case entity.CapabilityWebAuthorization:
			out = append(out, wechatv1.WechatCapability_WECHAT_CAPABILITY_WEB_AUTHORIZATION)
		case entity.CapabilityMiniProgramLogin:
			out = append(out, wechatv1.WechatCapability_WECHAT_CAPABILITY_MINI_PROGRAM_LOGIN)
		case entity.CapabilitySubscribeMessage:
			out = append(out, wechatv1.WechatCapability_WECHAT_CAPABILITY_SUBSCRIBE_MESSAGE)
		}
	}
	return out
}
