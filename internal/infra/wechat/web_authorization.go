package wechatinfra

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
)

// ExchangeWebAuthorization trades WeChat's code for an identity. The
// access token is used only to read a profile when the app asked for
// one, and is not returned. Which WeChat codes are a definitive refusal
// is left unset, so an error body is a business rejection.
func (c *openAPI) ExchangeWebAuthorization(ctx context.Context, req wechatservice.ExchangeWebAuthorizationRequest) (wechatservice.WebAuthorizationExchange, error) {
	token, identity, err := c.oauthIdentity(ctx, req)
	if errors.Is(err, context.Canceled) {
		return wechatservice.WebAuthorizationExchange{}, err
	}
	if err != nil || identity.OpenID == "" {
		return classifyWebExchange(err), nil
	}
	if !req.Profile {
		return identity, nil
	}
	nickname, avatar, unionID, err := c.oauthUserInfo(ctx, token, identity.OpenID)
	if errors.Is(err, context.Canceled) {
		return wechatservice.WebAuthorizationExchange{}, err
	}
	if err != nil {
		return classifyWebExchange(err), nil
	}
	identity.Nickname = nickname
	identity.AvatarURL = avatar
	if identity.UnionID == "" {
		identity.UnionID = unionID
	}
	return identity, nil
}

func (c *openAPI) oauthIdentity(ctx context.Context, req wechatservice.ExchangeWebAuthorizationRequest) (string, wechatservice.WebAuthorizationExchange, error) {
	query := url.Values{}
	query.Set("appid", req.AppID)
	query.Set("code", req.Code)
	query.Set("grant_type", "authorization_code")
	endpoint := c.base + "/sns/oauth2/access_token?"
	if req.Connection == entity.ConnectionPlatformAuthorization {
		componentToken, err := c.componentToken(ctx, req.ComponentAppID, req.ComponentSecret, req.VerifyTicket)
		if err != nil {
			return "", wechatservice.WebAuthorizationExchange{}, err
		}
		query.Set("component_appid", req.ComponentAppID)
		query.Set("component_access_token", componentToken)
		endpoint = c.base + "/sns/oauth2/component/access_token?"
	} else {
		query.Set("secret", req.AppSecret)
	}
	body, err := c.get(ctx, endpoint+query.Encode())
	if err != nil {
		return "", wechatservice.WebAuthorizationExchange{}, err
	}
	var resp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		OpenID      string `json:"openid"`
		UnionID     string `json:"unionid"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", wechatservice.WebAuthorizationExchange{}, errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 || resp.OpenID == "" || resp.AccessToken == "" {
		return "", wechatservice.WebAuthorizationExchange{}, openAPIFailure(resp.ErrMsg, "web authorization was not accepted")
	}
	return resp.AccessToken, wechatservice.WebAuthorizationExchange{
		Class:   wechatservice.CallSuccess,
		OpenID:  resp.OpenID,
		UnionID: resp.UnionID,
	}, nil
}

func (c *openAPI) oauthUserInfo(ctx context.Context, token, openID string) (string, string, string, error) {
	query := url.Values{}
	query.Set("access_token", token)
	query.Set("openid", openID)
	query.Set("lang", "zh_CN")
	body, err := c.get(ctx, c.base+"/sns/userinfo?"+query.Encode())
	if err != nil {
		return "", "", "", err
	}
	var resp struct {
		ErrCode    int    `json:"errcode"`
		ErrMsg     string `json:"errmsg"`
		Nickname   string `json:"nickname"`
		HeadImgURL string `json:"headimgurl"`
		UnionID    string `json:"unionid"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", "", errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 {
		return "", "", "", openAPIFailure(resp.ErrMsg, "web profile was not returned")
	}
	return resp.Nickname, resp.HeadImgURL, resp.UnionID, nil
}

func classifyWebExchange(err error) wechatservice.WebAuthorizationExchange {
	if err == nil || errors.Is(err, context.DeadlineExceeded) || probeUnavailable(err) {
		return wechatservice.WebAuthorizationExchange{Class: wechatservice.CallUnavailable, Message: "微信不可用"}
	}
	return wechatservice.WebAuthorizationExchange{Class: wechatservice.CallBusinessRejection, Message: "微信没有受理这次授权"}
}
