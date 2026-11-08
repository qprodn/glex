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
