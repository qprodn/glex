package wechatinfra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"glex/internal/domain/wechat/entity"
	wechatservice "glex/internal/domain/wechat/service"
)

const wechatAPIBase = "https://api.weixin.qq.com"

// openAPI is the WeChat Open API port. It classifies a call as success or
// failure and does not keep a table of WeChat error codes: a token comes
// back, or it does not. Nothing returned here is stored as a WeChat App.
type openAPI struct {
	client *http.Client
	base   string
}

// NewOpenAPI creates the process-wide Open API port. The host is WeChat's
// published API host. Redirects are not followed, so a token in a query
// string cannot be sent on to another host.
func NewOpenAPI() wechatservice.OpenAPI {
	return &openAPI{
		client: &http.Client{CheckRedirect: refuseRedirect},
		base:   wechatAPIBase,
	}
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func (c *openAPI) ProbePlatform(ctx context.Context, req wechatservice.ProbePlatformRequest) error {
	_, err := c.componentToken(ctx, req.ComponentAppID, req.ComponentSecret, req.VerifyTicket)
	return err
}

// MiniProgramLogin exchanges a one-time login code. The JSON WeChat
// returns includes session_key; this port never unmarshals that field,
// so the key is dropped and cannot be stored or returned. Which WeChat
// codes are a definitive refusal is left unset, so a WeChat error body
// stays a business rejection and a transport failure stays unavailable.
func (c *openAPI) MiniProgramLogin(ctx context.Context, req wechatservice.MiniProgramLoginCall) (wechatservice.MiniProgramLoginResult, error) {
	switch req.Connection {
	case entity.ConnectionDirectHold:
		openid, unionid, err := c.directCode2Session(ctx, req.AppID, req.AppSecret, req.Code)
		return classifyMiniProgramLogin(openid, unionid, err)
	case entity.ConnectionPlatformAuthorization:
		component, err := c.componentToken(ctx, req.ComponentAppID, req.ComponentSecret, req.VerifyTicket)
		if err != nil {
			return classifyMiniProgramLogin("", "", err)
		}
		openid, unionid, err := c.componentCode2Session(ctx, component, req.ComponentAppID, req.AppID, req.Code)
		return classifyMiniProgramLogin(openid, unionid, err)
	default:
		return wechatservice.MiniProgramLoginResult{Class: wechatservice.CallBusinessRejection}, nil
	}
}

func (c *openAPI) directCode2Session(ctx context.Context, appID, secret, code string) (string, string, error) {
	query := url.Values{}
	query.Set("appid", appID)
	query.Set("secret", secret)
	query.Set("js_code", code)
	query.Set("grant_type", "authorization_code")
	body, err := c.get(ctx, c.base+"/sns/jscode2session?"+query.Encode())
	if err != nil {
		return "", "", err
	}
	return readCode2Session(body)
}

func (c *openAPI) componentCode2Session(ctx context.Context, componentToken, componentAppID, appID, code string) (string, string, error) {
	query := url.Values{}
	query.Set("appid", appID)
	query.Set("js_code", code)
	query.Set("grant_type", "authorization_code")
	query.Set("component_appid", componentAppID)
	query.Set("component_access_token", componentToken)
	body, err := c.get(ctx, c.base+"/sns/component/jscode2session?"+query.Encode())
	if err != nil {
		return "", "", err
	}
	return readCode2Session(body)
}

// readCode2Session reads openid and unionid. session_key is not a field
// of the decoded value, so encoding/json discards it.
func readCode2Session(body []byte) (string, string, error) {
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		OpenID  string `json:"openid"`
		UnionID string `json:"unionid"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 || resp.OpenID == "" {
		return "", "", openAPIFailure(resp.ErrMsg, "mini program login was not accepted")
	}
	return resp.OpenID, resp.UnionID, nil
}

// classifyMiniProgramLogin turns a code exchange into the port's classes.
// A canceled caller is returned as-is. A WeChat error body is a business
// rejection because the definitive-refusal codes are unset. session_key
// never reaches the result.
func classifyMiniProgramLogin(openid, unionid string, err error) (wechatservice.MiniProgramLoginResult, error) {
	if errors.Is(err, context.Canceled) {
		return wechatservice.MiniProgramLoginResult{}, err
	}
	if err == nil && openid != "" {
		return wechatservice.MiniProgramLoginResult{
			Class:   wechatservice.CallSuccess,
			OpenID:  openid,
			UnionID: unionid,
		}, nil
	}
	if err == nil || errors.Is(err, context.DeadlineExceeded) || probeUnavailable(err) {
		return wechatservice.MiniProgramLoginResult{Class: wechatservice.CallUnavailable}, nil
	}
	return wechatservice.MiniProgramLoginResult{Class: wechatservice.CallBusinessRejection}, nil
}

// ProbeApp proves an app's current token. A Direct Hold asks for its own
// access token. A Platform Authorization refreshes the authorizer token
// through the platform. Neither call sends a message. Which WeChat codes
// are a definitive refusal is left unset, so a WeChat error body stays a
// business rejection and a transport failure stays unavailable. The domain
// lapses an app only when this port returns that class.
// PublishMenu replaces the default menu with the whole saved document.
// Which WeChat codes are a definitive refusal is left unset, so a WeChat
// error body stays a business rejection and a transport failure stays
// unavailable. The domain lapses an app only when this port returns that
// class.
func (c *openAPI) PublishMenu(ctx context.Context, req wechatservice.PublishMenuCall) (wechatservice.PublishMenuResult, error) {
	token, refresh, err := c.menuAccessToken(ctx, req)
	if errors.Is(err, context.Canceled) {
		return wechatservice.PublishMenuResult{}, err
	}
	if err != nil || token == "" {
		return classifyMenuPublish(false, refresh, err), nil
	}
	body, err := c.postJSON(ctx, c.base+"/cgi-bin/menu/create?access_token="+url.QueryEscape(token), menuCreateBody(req.Menu))
	if errors.Is(err, context.Canceled) {
		return wechatservice.PublishMenuResult{}, err
	}
	if err != nil {
		return classifyMenuPublish(false, refresh, err), nil
	}
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return classifyMenuPublish(false, refresh, errors.New("wechat open api: unreadable response")), nil
	}
	if resp.ErrCode != 0 {
		return classifyMenuPublish(false, refresh, openAPIFailure(resp.ErrMsg, "custom menu was not accepted")), nil
	}
	return classifyMenuPublish(true, refresh, nil), nil
}

func (c *openAPI) menuAccessToken(ctx context.Context, req wechatservice.PublishMenuCall) (string, string, error) {
	switch req.Connection {
	case entity.ConnectionDirectHold:
		token, err := c.clientCredentialToken(ctx, req.AppID, req.AppSecret)
		return token, "", err
	case entity.ConnectionPlatformAuthorization:
		component, err := c.componentToken(ctx, req.ComponentAppID, req.ComponentSecret, req.VerifyTicket)
		if err != nil {
			return "", "", err
		}
		return c.refreshAuthorizerToken(ctx, component, req.ComponentAppID, req.AppID, req.AuthorizerRefreshToken)
	default:
		return "", "", errors.New("wechat open api: unknown connection")
	}
}

func classifyMenuPublish(accepted bool, refresh string, err error) wechatservice.PublishMenuResult {
	if err == nil && accepted {
		return wechatservice.PublishMenuResult{
			Class:                  wechatservice.CallSuccess,
			Message:                "已替换默认菜单",
			AuthorizerRefreshToken: refresh,
		}
	}
	if err == nil || errors.Is(err, context.DeadlineExceeded) || probeUnavailable(err) {
		return wechatservice.PublishMenuResult{Class: wechatservice.CallUnavailable, Message: "微信不可用", AuthorizerRefreshToken: refresh}
	}
	return wechatservice.PublishMenuResult{Class: wechatservice.CallBusinessRejection, Message: "微信没有受理这次调用", AuthorizerRefreshToken: refresh}
}

func menuCreateBody(menu entity.CustomMenu) map[string]any {
	return map[string]any{"button": menuCreateButtons(menu.Buttons)}
}

func menuCreateButtons(buttons []entity.MenuButton) []map[string]any {
	out := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		if len(button.SubButtons) > 0 {
			out = append(out, map[string]any{
				"name":       button.Name,
				"sub_button": menuCreateButtons(button.SubButtons),
			})
			continue
		}
		if button.MiniProgramAppID != "" {
			out = append(out, map[string]any{
				"type":     "miniprogram",
				"name":     button.Name,
				"appid":    button.MiniProgramAppID,
				"pagepath": button.MiniProgramPage,
				"url":      "https://mp.weixin.qq.com",
			})
			continue
		}
		out = append(out, map[string]any{
			"type": "view",
			"name": button.Name,
			"url":  button.WebURL,
		})
	}
	return out
}

func (c *openAPI) ProbeApp(ctx context.Context, req wechatservice.ProbeAppRequest) (wechatservice.ProbeAppResult, error) {
	switch req.Connection {
	case entity.ConnectionDirectHold:
		token, err := c.clientCredentialToken(ctx, req.AppID, req.AppSecret)
		return classifyAppProbe(token != "", "", err)
	case entity.ConnectionPlatformAuthorization:
		component, err := c.componentToken(ctx, req.ComponentAppID, req.ComponentSecret, req.VerifyTicket)
		if err != nil {
			return classifyAppProbe(false, "", err)
		}
		access, refresh, err := c.refreshAuthorizerToken(ctx, component, req.ComponentAppID, req.AppID, req.AuthorizerRefreshToken)
		return classifyAppProbe(access != "", refresh, err)
	default:
		return wechatservice.ProbeAppResult{Class: wechatservice.CallBusinessRejection, Message: "未知接入方式"}, nil
	}
}

func (c *openAPI) clientCredentialToken(ctx context.Context, appID, secret string) (string, error) {
	query := url.Values{}
	query.Set("grant_type", "client_credential")
	query.Set("appid", appID)
	query.Set("secret", secret)
	body, err := c.get(ctx, c.base+"/cgi-bin/token?"+query.Encode())
	if err != nil {
		return "", err
	}
	var resp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 || resp.AccessToken == "" {
		return "", openAPIFailure(resp.ErrMsg, "access token was not issued")
	}
	return resp.AccessToken, nil
}

func (c *openAPI) refreshAuthorizerToken(ctx context.Context, componentToken, componentAppID, authorizerAppID, refreshToken string) (string, string, error) {
	endpoint := c.base + "/cgi-bin/component/api_authorizer_token?component_access_token=" + url.QueryEscape(componentToken)
	body, err := c.postJSON(ctx, endpoint, map[string]string{
		"component_appid":          componentAppID,
		"authorizer_appid":         authorizerAppID,
		"authorizer_refresh_token": refreshToken,
	})
	if err != nil {
		return "", "", err
	}
	var resp struct {
		ErrCode                int    `json:"errcode"`
		ErrMsg                 string `json:"errmsg"`
		AuthorizerAccessToken  string `json:"authorizer_access_token"`
		AuthorizerRefreshToken string `json:"authorizer_refresh_token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 || resp.AuthorizerAccessToken == "" {
		return "", "", openAPIFailure(resp.ErrMsg, "authorizer token was not issued")
	}
	return resp.AuthorizerAccessToken, resp.AuthorizerRefreshToken, nil
}

// classifyAppProbe turns a token attempt into the port's four classes.
// A canceled caller is returned as-is. A WeChat error body is a business
// rejection because the definitive-refusal codes are unset.
func classifyAppProbe(issued bool, refresh string, err error) (wechatservice.ProbeAppResult, error) {
	if errors.Is(err, context.Canceled) {
		return wechatservice.ProbeAppResult{}, err
	}
	if err == nil && issued {
		return wechatservice.ProbeAppResult{
			Class:                  wechatservice.CallSuccess,
			Message:                "连接成功",
			AuthorizerRefreshToken: refresh,
		}, nil
	}
	if err == nil || errors.Is(err, context.DeadlineExceeded) || probeUnavailable(err) {
		return wechatservice.ProbeAppResult{Class: wechatservice.CallUnavailable, Message: "微信不可用"}, nil
	}
	return wechatservice.ProbeAppResult{Class: wechatservice.CallBusinessRejection, Message: "微信没有受理这次调用"}, nil
}

func probeUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "request failed") || strings.Contains(msg, "unreadable") || strings.Contains(msg, "http ")
}

func (c *openAPI) StartAuthorization(ctx context.Context, req wechatservice.OpenAuthorizationStart) (*wechatservice.AuthorizationLink, error) {
	componentToken, err := c.componentToken(ctx, req.ComponentAppID, req.ComponentSecret, req.VerifyTicket)
	if err != nil {
		return nil, err
	}
	preAuthCode, err := c.preAuthCode(ctx, componentToken, req.ComponentAppID)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("component_appid", req.ComponentAppID)
	query.Set("pre_auth_code", preAuthCode)
	query.Set("redirect_uri", req.RedirectURL)
	// auth_type 3 asks WeChat to offer both an official account and a mini
	// program. It is not a permission-set id; those ids are left unset, so
	// the link does not name a category list.
	query.Set("auth_type", "3")
	return &wechatservice.AuthorizationLink{
		URL: "https://mp.weixin.qq.com/cgi-bin/componentloginpage?" + query.Encode(),
	}, nil
}

func (c *openAPI) CompleteAuthorization(ctx context.Context, req wechatservice.OpenAuthorizationCode) (*wechatservice.AuthorizationGrant, error) {
	componentToken, err := c.componentToken(ctx, req.ComponentAppID, req.ComponentSecret, req.VerifyTicket)
	if err != nil {
		return nil, err
	}
	queried, err := c.queryAuthorization(ctx, componentToken, req.ComponentAppID, req.AuthCode)
	if err != nil {
		return nil, err
	}
	profile, err := c.authorizerProfile(ctx, componentToken, req.ComponentAppID, queried.appID)
	if err != nil {
		return nil, err
	}
	return &wechatservice.AuthorizationGrant{
		AppID:        queried.appID,
		Name:         profile.name,
		AppType:      profile.appType,
		Kind:         profile.kind,
		Capabilities: classifyGranted(queried.funcIDs, profile.appType),
		RefreshToken: queried.refreshToken,
	}, nil
}

func (c *openAPI) AnswerFullNetwork(ctx context.Context, req wechatservice.FullNetworkReply) error {
	componentToken, err := c.componentToken(ctx, req.ComponentAppID, req.ComponentSecret, req.VerifyTicket)
	if err != nil {
		return err
	}
	authorizerToken, err := c.authorizerToken(ctx, componentToken, req.ComponentAppID, req.QueryAuthCode)
	if err != nil {
		return err
	}
	return c.sendCustomerText(ctx, authorizerToken, req.ToUser, req.Content)
}

func (c *openAPI) componentToken(ctx context.Context, appID, secret, ticket string) (string, error) {
	body, err := c.postJSON(ctx, c.base+"/cgi-bin/component/api_component_token", map[string]string{
		"component_appid":         appID,
		"component_appsecret":     secret,
		"component_verify_ticket": ticket,
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		ErrCode              int    `json:"errcode"`
		ErrMsg               string `json:"errmsg"`
		ComponentAccessToken string `json:"component_access_token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 || resp.ComponentAccessToken == "" {
		return "", openAPIFailure(resp.ErrMsg, "component token was not issued")
	}
	return resp.ComponentAccessToken, nil
}

func (c *openAPI) authorizerToken(ctx context.Context, componentToken, appID, code string) (string, error) {
	endpoint := c.base + "/cgi-bin/component/api_query_auth?component_access_token=" + url.QueryEscape(componentToken)
	body, err := c.postJSON(ctx, endpoint, map[string]string{
		"component_appid":    appID,
		"authorization_code": code,
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		ErrCode           int    `json:"errcode"`
		ErrMsg            string `json:"errmsg"`
		AuthorizationInfo struct {
			AuthorizerAccessToken string `json:"authorizer_access_token"`
		} `json:"authorization_info"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", errors.New("wechat open api: unreadable response")
	}
	token := resp.AuthorizationInfo.AuthorizerAccessToken
	if resp.ErrCode != 0 || token == "" {
		return "", openAPIFailure(resp.ErrMsg, "authorization was not issued")
	}
	return token, nil
}

func (c *openAPI) preAuthCode(ctx context.Context, componentToken, appID string) (string, error) {
	endpoint := c.base + "/cgi-bin/component/api_create_preauthcode?component_access_token=" + url.QueryEscape(componentToken)
	body, err := c.postJSON(ctx, endpoint, map[string]string{"component_appid": appID})
	if err != nil {
		return "", err
	}
	var resp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		PreAuthCode string `json:"pre_auth_code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 || resp.PreAuthCode == "" {
		return "", openAPIFailure(resp.ErrMsg, "pre-auth code was not issued")
	}
	return resp.PreAuthCode, nil
}

type queriedAuthorization struct {
	appID        string
	refreshToken string
	funcIDs      []int
}

func (c *openAPI) queryAuthorization(ctx context.Context, componentToken, appID, code string) (queriedAuthorization, error) {
	endpoint := c.base + "/cgi-bin/component/api_query_auth?component_access_token=" + url.QueryEscape(componentToken)
	body, err := c.postJSON(ctx, endpoint, map[string]string{
		"component_appid":    appID,
		"authorization_code": code,
	})
	if err != nil {
		return queriedAuthorization{}, err
	}
	var resp struct {
		ErrCode           int    `json:"errcode"`
		ErrMsg            string `json:"errmsg"`
		AuthorizationInfo struct {
			AuthorizerAppid        string `json:"authorizer_appid"`
			AuthorizerRefreshToken string `json:"authorizer_refresh_token"`
			FuncInfo               []struct {
				FuncscopeCategory struct {
					ID int `json:"id"`
				} `json:"funcscope_category"`
			} `json:"func_info"`
		} `json:"authorization_info"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return queriedAuthorization{}, errors.New("wechat open api: unreadable response")
	}
	info := resp.AuthorizationInfo
	if resp.ErrCode != 0 || info.AuthorizerAppid == "" || info.AuthorizerRefreshToken == "" {
		return queriedAuthorization{}, openAPIFailure(resp.ErrMsg, "authorization was not issued")
	}
	ids := make([]int, 0, len(info.FuncInfo))
	for _, item := range info.FuncInfo {
		ids = append(ids, item.FuncscopeCategory.ID)
	}
	return queriedAuthorization{
		appID:        info.AuthorizerAppid,
		refreshToken: info.AuthorizerRefreshToken,
		funcIDs:      ids,
	}, nil
}

type authorizerProfileResult struct {
	name    string
	appType string
	kind    string
}

func (c *openAPI) authorizerProfile(ctx context.Context, componentToken, componentAppID, authorizerAppID string) (authorizerProfileResult, error) {
	endpoint := c.base + "/cgi-bin/component/api_get_authorizer_info?component_access_token=" + url.QueryEscape(componentToken)
	body, err := c.postJSON(ctx, endpoint, map[string]string{
		"component_appid":  componentAppID,
		"authorizer_appid": authorizerAppID,
	})
	if err != nil {
		return authorizerProfileResult{}, err
	}
	var resp struct {
		ErrCode        int    `json:"errcode"`
		ErrMsg         string `json:"errmsg"`
		AuthorizerInfo struct {
			NickName        string `json:"nick_name"`
			ServiceTypeInfo struct {
				ID int `json:"id"`
			} `json:"service_type_info"`
			MiniProgramInfo json.RawMessage `json:"MiniProgramInfo"`
		} `json:"authorizer_info"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return authorizerProfileResult{}, errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 || resp.AuthorizerInfo.NickName == "" {
		return authorizerProfileResult{}, openAPIFailure(resp.ErrMsg, "authorizer profile was not returned")
	}
	if miniProgram(resp.AuthorizerInfo.MiniProgramInfo) {
		return authorizerProfileResult{
			name:    resp.AuthorizerInfo.NickName,
			appType: entity.AppTypeMiniProgram,
		}, nil
	}
	kind := ""
	switch resp.AuthorizerInfo.ServiceTypeInfo.ID {
	case 0, 1:
		kind = entity.AccountKindSubscription
	case 2:
		kind = entity.AccountKindService
	}
	return authorizerProfileResult{
		name:    resp.AuthorizerInfo.NickName,
		appType: entity.AppTypeOfficialAccount,
		kind:    kind,
	}, nil
}

func miniProgram(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

// publishedCapability is WeChat's published permission-set id for one
// capability this phase stores by name. Ids outside the table are ignored,
// so a permission-set number is never stored on the app. The authorization
// link does not request a category list; which ids WeChat offers is left
// to the platform's own WeChat configuration.
var publishedCapability = map[int]string{
	4:  entity.CapabilityWebAuthorization,      // 网页服务
	7:  entity.CapabilityTemplateMessage,       // 群发与通知，含模板消息
	15: entity.CapabilityCustomMenu,            // 菜单管理
	18: entity.CapabilitySubscribeMessage,      // 小程序开发与数据分析，含发送订阅消息
	89: entity.CapabilitySubscribeNotification, // 服务号订阅通知
}

func classifyGranted(funcIDs []int, appType string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(funcIDs)+1)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, id := range funcIDs {
		add(publishedCapability[id])
	}
	if appType == entity.AppTypeMiniProgram {
		add(entity.CapabilityMiniProgramLogin)
	}
	return out
}

func (c *openAPI) sendCustomerText(ctx context.Context, authorizerToken, toUser, content string) error {
	endpoint := c.base + "/cgi-bin/message/custom/send?access_token=" + url.QueryEscape(authorizerToken)
	body, err := c.postJSON(ctx, endpoint, map[string]any{
		"touser":  toUser,
		"msgtype": "text",
		"text":    map[string]string{"content": content},
	})
	if err != nil {
		return err
	}
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errors.New("wechat open api: unreadable response")
	}
	if resp.ErrCode != 0 {
		return openAPIFailure(resp.ErrMsg, "customer message was not accepted")
	}
	return nil
}

func (c *openAPI) get(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, openAPITransport(err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		// The endpoint may carry an app secret in the query string.
		// The raw client error quotes that URL, so it does not cross
		// this boundary.
		return nil, openAPITransport(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, openAPITransport(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wechat open api: http %d", resp.StatusCode)
	}
	return body, nil
}

func (c *openAPI) postJSON(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("wechat open api: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, openAPITransport(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		// The endpoint may carry an access token in the query string.
		// The raw client error quotes that URL, so it does not cross
		// this boundary.
		return nil, openAPITransport(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, openAPITransport(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wechat open api: http %d", resp.StatusCode)
	}
	return body, nil
}

func openAPITransport(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("wechat open api: request failed")
}

func openAPIFailure(errmsg, fallback string) error {
	errmsg = strings.TrimSpace(errmsg)
	if errmsg == "" || errmsg == "ok" {
		return errors.New("wechat open api: " + fallback)
	}
	return errors.New("wechat open api: " + errmsg)
}
