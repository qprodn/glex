package wechat

import (
	"context"

	wechatv1 "glex/api/wechat/v1"
	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
	"glex/pkg/logx"
)

func webAuthScopeProto(v string) wechatv1.WechatWebAuthScope {
	switch v {
	case entity.WebAuthScopeProfile:
		return wechatv1.WechatWebAuthScope_WECHAT_WEB_AUTH_SCOPE_PROFILE
	default:
		return wechatv1.WechatWebAuthScope_WECHAT_WEB_AUTH_SCOPE_OPENID
	}
}

func webAuthScopeDomain(v wechatv1.WechatWebAuthScope) string {
	switch v {
	case wechatv1.WechatWebAuthScope_WECHAT_WEB_AUTH_SCOPE_PROFILE:
		return entity.WebAuthScopeProfile
	default:
		return entity.WebAuthScopeOpenID
	}
}

// ConfigureWechatWebAuthorization stores the return URL and the scope.
// update_by is the authenticated Principal.
func (s *WechatApplication) ConfigureWechatWebAuthorization(ctx context.Context, req *wechatv1.ConfigureWechatWebAuthorizationRequest) (*wechatv1.WechatApp, error) {
	updated, err := s.apps.ConfigureWebAuthorization(ctx, wechatservice.ConfigureWebAuthorizationRequest{
		ID:        req.GetAppId(),
		ReturnURL: req.GetReturnUrl(),
		Scope:     webAuthScopeDomain(req.GetWebAuthScope()),
		ActorID:   s.actor(ctx),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.configure_web_authorization", err)
		return nil, err
	}
	return convertApp(updated), nil
}

// StartWechatWebAuthorization returns the WeChat address the browser
// opens. It does not require a back-office Principal. The request's
// return URL is passed through and ignored.
func (s *WechatApplication) StartWechatWebAuthorization(ctx context.Context, req *wechatv1.StartWechatWebAuthorizationRequest) (*wechatv1.WechatWebAuthorizationStart, error) {
	started, err := s.apps.StartWebAuthorization(ctx, wechatservice.StartWebAuthorizationRequest{
		ID:        req.GetAppId(),
		ReturnURL: req.GetReturnUrl(),
	})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.start_web_authorization", err)
		return nil, err
	}
	return &wechatv1.WechatWebAuthorizationStart{AuthorizeUrl: started.AuthorizeURL}, nil
}

// AcceptWebAuthorization is the browser callback after WeChat accepts
// the grant. The landing location is the return URL stored on the app.
func (s *WechatApplication) AcceptWebAuthorization(ctx context.Context, req wechatservice.AcceptWebAuthorizationRequest) (*wechatservice.WebAuthorizationLanding, error) {
	landing, err := s.apps.AcceptWebAuthorization(ctx, req)
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.accept_web_authorization", err)
		return nil, err
	}
	return landing, nil
}

// RedeemWechatHandoffCode spends one Handoff Code. It does not require a
// back-office Principal and writes no Login Log.
func (s *WechatApplication) RedeemWechatHandoffCode(ctx context.Context, req *wechatv1.RedeemWechatHandoffCodeRequest) (*wechatv1.WechatWebIdentity, error) {
	identity, err := s.apps.RedeemHandoffCode(ctx, wechatservice.RedeemHandoffCodeRequest{Code: req.GetCode()})
	if err != nil {
		logx.Propagate(ctx, s.logger, "wechat.redeem_handoff_code", err)
		return nil, err
	}
	return &wechatv1.WechatWebIdentity{
		Openid:    identity.OpenID,
		Unionid:   identity.UnionID,
		Nickname:  identity.Nickname,
		AvatarUrl: identity.AvatarURL,
	}, nil
}
