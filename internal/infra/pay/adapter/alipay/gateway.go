package alipay

import (
	"errors"

	"glex/internal/infra/pay/adapter"
	payservice "glex/internal/domain/pay/service"
)

// newGateway builds the alipay gateway; the implementation lands with the
// adapter's contract tests (ticket 04).
func newGateway(cfg map[string]string, opts ...adapter.GatewayOption) (payservice.ChannelGateway, error) {
	return nil, errNotImplemented
}

var errNotImplemented = errors.New("alipay: gateway not implemented yet")
