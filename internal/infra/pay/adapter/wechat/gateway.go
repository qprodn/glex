package wechat

import (
	"glex/internal/infra/pay/adapter"
	payservice "glex/internal/domain/pay/service"
)

// newGateway builds the wechat gateway; the implementation lands with the
// adapter's contract tests (ticket 03).
func newGateway(cfg map[string]string, opts ...adapter.GatewayOption) (payservice.ChannelGateway, error) {
	return nil, errGatewayNotImplemented
}
