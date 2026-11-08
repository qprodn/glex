package server

import (
	"context"
	"io"
	stdhttp "net/http"
	"strconv"

	wechatapp "glex/internal/application/wechat"
	wechat "glex/internal/domain/wechat"
	wechatservice "glex/internal/domain/wechat/service"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// OperationWechatServicePlatformCallback names the hand-written platform
// callback. It is absent from the auth route table — WeChat's encrypted
// push is the authentication — and present in the operlog skip set: no
// Principal rides a WeChat push, and the push is not a back-office mutation.
const OperationWechatServicePlatformCallback = "/wechat.v1.WechatService/PlatformCallback"

// maxWechatPushBytes bounds one WeChat push before it is read. The route
// is public; the signature inside the body is the auth. Encrypted XML
// pushes are a few KiB.
const maxWechatPushBytes = 64 << 10

// registerWechatCallbackHTTP wires one callback address per platform:
// POST /wechat/platform/{platform_id}/callback
// and the message URL WeChat fills as
// POST /wechat/platform/{platform_id}/callback/{appid}.
func registerWechatCallbackHTTP(s *khttp.Server, app *wechatapp.WechatApplication) {
	if app == nil {
		return
	}
	r := s.Route("/")
	r.Handle("POST", "/wechat/platform/{platform_id}/callback", wechatCallbackHandler(app))
	r.Handle("POST", "/wechat/platform/{platform_id}/callback/{appid}", wechatCallbackHandler(app))
	r.Handle("GET", "/wechat/platform/{platform_id}/authorization-return", authorizationReturnHandler())
	r.Handle("GET", "/wechat/app/{app_id}/web-authorization/return", webAuthorizationReturnHandler(app))
}

// OperationWechatServiceAuthorizationReturn names the browser page WeChat
// redirects to after an administrator grants a platform. The page stores
// nothing: the encrypted authorized push on the platform callback creates
// the WeChat App. The route is absent from the auth table.
const OperationWechatServiceAuthorizationReturn = "/wechat.v1.WechatService/AuthorizationReturn"

const authorizationReturnHTML = `<!DOCTYPE html>
<meta charset="utf-8">
<title>授权已提交</title>
<p>授权已提交。请回到后台的微信应用列表查看。应用由微信返回的结果建立，这个页面不填写 AppID，也不保存对方的 AppSecret。</p>
`

func authorizationReturnHandler() khttp.HandlerFunc {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, OperationWechatServiceAuthorizationReturn)
		h := ctx.Middleware(func(context.Context, any) (any, error) {
			return nil, nil
		})
		if _, err := h(ctx, &struct{}{}); err != nil {
			return err
		}
		ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		ctx.Response().WriteHeader(stdhttp.StatusOK)
		_, _ = ctx.Response().Write([]byte(authorizationReturnHTML))
		return nil
	}
}

func wechatCallbackHandler(app *wechatapp.WechatApplication) khttp.HandlerFunc {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, OperationWechatServicePlatformCallback)
		platformID, parseErr := strconv.ParseInt(ctx.Vars().Get("platform_id"), 10, 64)
		if parseErr != nil || platformID <= 0 {
			return wechat.ErrPushRejected
		}
		body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, maxWechatPushBytes+1))
		if err != nil || len(body) > maxWechatPushBytes {
			return wechat.ErrPushRejected
		}
		query := ctx.Request().URL.Query()
		h := ctx.Middleware(func(requestCtx context.Context, _ any) (any, error) {
			return app.ReceivePlatformPush(requestCtx, &wechatservice.PushRequest{
				PlatformID:      platformID,
				AuthorizerAppID: ctx.Vars().Get("appid"),
				Timestamp:       query.Get("timestamp"),
				Nonce:           query.Get("nonce"),
				Signature:       query.Get("signature"),
				MsgSignature:    query.Get("msg_signature"),
				EncryptType:     query.Get("encrypt_type"),
				Body:            body,
			})
		})
		out, err := h(ctx, &struct{}{})
		if err != nil {
			return err
		}
		outcome := out.(*wechatservice.PushOutcome)
		contentType := outcome.ContentType
		if contentType == "" {
			contentType = "text/plain; charset=utf-8"
		}
		ctx.Response().Header().Set("Content-Type", contentType)
		ctx.Response().WriteHeader(stdhttp.StatusOK)
		_, _ = ctx.Response().Write([]byte(outcome.Body))
		return nil
	}
}
