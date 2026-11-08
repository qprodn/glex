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
