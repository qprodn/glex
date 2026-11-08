package server

import (
	"context"
	stdhttp "net/http"
	"strconv"

	wechatapp "glex/internal/application/wechat"
	wechat "glex/internal/domain/wechat"
	wechatservice "glex/internal/domain/wechat/service"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// OperationWechatServiceWebAuthorizationReturn names the browser URL
// WeChat redirects to after a person grants Web Authorization. It is
// absent from the auth route table and present in the operlog skip set:
// no Principal rides the redirect, and it is not a back-office mutation.
const OperationWechatServiceWebAuthorizationReturn = "/wechat.v1.WechatService/WebAuthorizationReturn"

func webAuthorizationReturnHandler(app *wechatapp.WechatApplication) khttp.HandlerFunc {
	return func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, OperationWechatServiceWebAuthorizationReturn)
		appID, parseErr := strconv.ParseInt(ctx.Vars().Get("app_id"), 10, 64)
		if parseErr != nil || appID <= 0 || app == nil {
			return wechat.ErrWebAuthorizationRefused
		}
		query := ctx.Request().URL.Query()
		h := ctx.Middleware(func(requestCtx context.Context, _ any) (any, error) {
			return app.AcceptWebAuthorization(requestCtx, wechatservice.AcceptWebAuthorizationRequest{
				ID:        appID,
				Code:      query.Get("code"),
				ReturnURL: query.Get("return_url"),
			})
		})
		out, err := h(ctx, &struct{}{})
		if err != nil {
			return err
		}
		landing := out.(*wechatservice.WebAuthorizationLanding)
		stdhttp.Redirect(ctx.Response(), ctx.Request(), landing.Location, stdhttp.StatusFound)
		return nil
	}
}
