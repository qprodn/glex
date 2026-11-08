package service

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	wechat "glex/internal/domain/wechat"
	"glex/internal/domain/wechat/entity"
	"glex/internal/domain/wechat/repository"
	"glex/pkg/logx"
)

// ConfigureWebAuthorizationRequest stores the return URL and the scope
// on one Service Account. Scope is openid only, or nickname and avatar
// as well. The actor is stamped into update_by.
type ConfigureWebAuthorizationRequest struct {
	ID        int64
	ReturnURL string
	Scope     string
	ActorID   int64
}

func (s *appImpl) ConfigureWebAuthorization(ctx context.Context, req ConfigureWebAuthorizationRequest) (*entity.App, error) {
	const op = "wechat.svc.app.configure_web_authorization"
	if req.Scope != entity.WebAuthScopeOpenID && req.Scope != entity.WebAuthScopeProfile {
		err := wechat.ErrAppInvalid
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	current, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if !serviceAccount(current) {
		err = wechat.ErrWebAuthorizationRefused
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	returnURL, err := webReturnURL(req.ReturnURL)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	current.ReturnURL = returnURL
	current.WebAuthScope = req.Scope
	current.UpdateBy = req.ActorID
	updated, err := s.apps.Update(ctx, repository.SaveAppRequest{App: current, ActorID: req.ActorID})
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	return present(updated), nil
}

// StartWebAuthorizationRequest asks to begin Web Authorization for one
// app. ReturnURL is not where the browser lands. That address is the
// one stored on the Service Account.
type StartWebAuthorizationRequest struct {
	ID        int64
	ReturnURL string
}

// WebAuthorizationStart is the WeChat address the browser opens.
type WebAuthorizationStart struct {
	AuthorizeURL string
}

func (s *appImpl) StartWebAuthorization(ctx context.Context, req StartWebAuthorizationRequest) (*WebAuthorizationStart, error) {
	const op = "wechat.svc.app.start_web_authorization"
	current, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	componentAppID, err := s.webAuthorizationAllowed(ctx, current, op)
	if err != nil {
		return nil, err
	}
	return &WebAuthorizationStart{AuthorizeURL: authorizeURL(s.callbackBase, current, componentAppID)}, nil
}

// AcceptWebAuthorizationRequest is WeChat's completed grant. Code is the
// code WeChat put on our callback. ReturnURL is ignored: the browser
// lands on the URL stored on the Service Account.
type AcceptWebAuthorizationRequest struct {
	ID        int64
	Code      string
	ReturnURL string
}

// WebAuthorizationLanding is where the browser goes, with one Handoff Code.
type WebAuthorizationLanding struct {
	Location string
}

func (s *appImpl) AcceptWebAuthorization(ctx context.Context, req AcceptWebAuthorizationRequest) (*WebAuthorizationLanding, error) {
	const op = "wechat.svc.app.accept_web_authorization"
	if strings.TrimSpace(req.Code) == "" {
		err := wechat.ErrWebAuthorizationRefused
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	current, err := s.apps.FindByID(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	if _, err := s.webAuthorizationAllowed(ctx, current, op); err != nil {
		return nil, err
	}
	if _, err := landingLocation(current.ReturnURL, "pending"); err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	exchange, err := s.webExchangeRequest(ctx, current, req.Code, op)
	if err != nil {
		return nil, err
	}
	if s.openapi == nil || s.handoffs == nil {
		err = wechat.ErrWebAuthorizationRefused
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	result, err := s.openapi.ExchangeWebAuthorization(ctx, exchange)
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	if err != nil {
		err = wechat.ErrWebAuthorizationRefused
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	switch result.Class {
	case CallSuccess:
		if result.OpenID == "" {
			err = wechat.ErrWebAuthorizationRefused
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		profile := current.WebAuthScope == entity.WebAuthScopeProfile
		grant := repository.HandoffGrant{OpenID: result.OpenID, UnionID: result.UnionID, Profile: profile}
		if profile {
			grant.Nickname = result.Nickname
			grant.AvatarURL = result.AvatarURL
		}
		code, err := s.handoffs.Issue(ctx, grant)
		if err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		location, err := landingLocation(current.ReturnURL, code)
		if err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		return &WebAuthorizationLanding{Location: location}, nil
	case CallDefinitiveRefusal:
		if err := s.lapse(ctx, current); err != nil {
			logx.Propagate(ctx, s.logger, op, err)
			return nil, err
		}
		err = wechat.ErrWebAuthorizationRefused
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	default:
		err = wechat.ErrWebAuthorizationRefused
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
}

// webAuthorizationAllowed reports whether this app can run Web
// Authorization. The component AppID is empty for a Direct Hold. A
// stored return URL is required. The address on the request is not
// consulted. readyPlatform logs its own refusal.
func (s *appImpl) webAuthorizationAllowed(ctx context.Context, app *entity.App, op string) (string, error) {
	if app.Lapsed {
		err := wechat.ErrAppLapsed
		logx.Propagate(ctx, s.logger, op, err)
		return "", err
	}
	if !serviceAccount(app) || app.Suspended || strings.TrimSpace(app.ReturnURL) == "" {
		err := wechat.ErrWebAuthorizationRefused
		logx.Propagate(ctx, s.logger, op, err)
		return "", err
	}
	if app.Connection != entity.ConnectionPlatformAuthorization {
		return "", nil
	}
	if !hasCapability(app, entity.CapabilityWebAuthorization) {
		err := wechat.ErrWebAuthorizationRefused
		logx.Propagate(ctx, s.logger, op, err)
		return "", err
	}
	p, _, _, err := s.readyPlatform(ctx, app.PlatformID, op)
	if err != nil {
		return "", err
	}
	return p.ComponentAppID, nil
}

func (s *appImpl) webExchangeRequest(ctx context.Context, app *entity.App, code, op string) (ExchangeWebAuthorizationRequest, error) {
	secrets, err := s.apps.LoadSecrets(ctx, app.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return ExchangeWebAuthorizationRequest{}, err
	}
	req := ExchangeWebAuthorizationRequest{
		Connection: app.Connection,
		AppID:      app.AppID,
		Code:       code,
		Profile:    app.WebAuthScope == entity.WebAuthScopeProfile,
	}
	if app.Connection == entity.ConnectionDirectHold {
		req.AppSecret = secrets.AppSecret
		return req, nil
	}
	p, platformSecrets, ticket, err := s.readyPlatform(ctx, app.PlatformID, op)
	if err != nil {
		return ExchangeWebAuthorizationRequest{}, err
	}
	req.ComponentAppID = p.ComponentAppID
	req.ComponentSecret = platformSecrets.ComponentSecret
	req.VerifyTicket = ticket
	req.AuthorizerRefreshToken = secrets.AuthorizerRefreshToken
	return req, nil
}

// RedeemHandoffCodeRequest spends one Handoff Code.
type RedeemHandoffCodeRequest struct {
	Code string
}

// WebIdentity is the openid and unionid from one Web Authorization, plus
// nickname and avatar only when that app asked for them.
type WebIdentity struct {
	OpenID    string
	UnionID   string
	Nickname  string
	AvatarURL string
}

func (s *appImpl) RedeemHandoffCode(ctx context.Context, req RedeemHandoffCodeRequest) (*WebIdentity, error) {
	const op = "wechat.svc.app.redeem_handoff_code"
	if s.handoffs == nil || strings.TrimSpace(req.Code) == "" {
		err := wechat.ErrHandoffCodeInvalid
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	grant, err := s.handoffs.Consume(ctx, strings.TrimSpace(req.Code))
	if err != nil {
		logx.Propagate(ctx, s.logger, op, err)
		return nil, err
	}
	identity := &WebIdentity{OpenID: grant.OpenID, UnionID: grant.UnionID}
	if grant.Profile {
		identity.Nickname = grant.Nickname
		identity.AvatarURL = grant.AvatarURL
	}
	return identity, nil
}

func authorizeURL(callbackBase string, app *entity.App, componentAppID string) string {
	query := url.Values{}
	query.Set("appid", app.AppID)
	query.Set("redirect_uri", strings.TrimRight(callbackBase, "/")+"/wechat/app/"+strconv.FormatInt(app.ID, 10)+"/web-authorization/return")
	query.Set("response_type", "code")
	if app.WebAuthScope == entity.WebAuthScopeProfile {
		query.Set("scope", "snsapi_userinfo")
	} else {
		query.Set("scope", "snsapi_base")
	}
	query.Set("state", "web")
	if componentAppID != "" {
		query.Set("component_appid", componentAppID)
	}
	return (&url.URL{
		Scheme:   "https",
		Host:     "open.weixin.qq.com",
		Path:     "/connect/oauth2/authorize",
		RawQuery: query.Encode(),
		Fragment: "wechat_redirect",
	}).String()
}

func webReturnURL(raw string) (string, error) {
	returnURL := strings.TrimSpace(raw)
	if returnURL == "" {
		return "", nil
	}
	if len(returnURL) > 2048 {
		return "", wechat.ErrAppInvalid
	}
	parsed, err := url.Parse(returnURL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", wechat.ErrAppInvalid
	}
	return returnURL, nil
}

func landingLocation(returnURL, code string) (string, error) {
	u, err := url.Parse(returnURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", wechat.ErrWebAuthorizationRefused
	}
	query := u.Query()
	query.Set("handoff_code", code)
	u.RawQuery = query.Encode()
	u.Fragment = ""
	return u.String(), nil
}

func hasCapability(app *entity.App, capability string) bool {
	for _, granted := range app.Capabilities {
		if granted == capability {
			return true
		}
	}
	return false
}

func serviceAccount(app *entity.App) bool {
	return app != nil && app.AppType == entity.AppTypeOfficialAccount && app.Kind == entity.AccountKindService
}
