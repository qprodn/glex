package wechat

import (
	"log/slog"

	wechatv1 "glex/api/wechat/v1"
	wechatservice "glex/internal/domain/wechat/service"
)

// WechatApplication implements the wechat management service. It owns the
// module's use cases; all row access goes through the wechat domain service
// (ADR 0005).
type WechatApplication struct {
	wechatv1.UnimplementedWechatServiceServer

	platforms wechatservice.Platform
	apps      wechatservice.App
	tx        Transaction
	logger    *slog.Logger
}

// NewWechatApplication creates the WechatApplication.
func NewWechatApplication(platforms wechatservice.Platform, apps wechatservice.App, tx Transaction, logger *slog.Logger) *WechatApplication {
	return &WechatApplication{
		platforms: platforms,
		apps:      apps,
		tx:        tx,
		logger:    logger.With(slog.String("component", "application.wechat")),
	}
}
