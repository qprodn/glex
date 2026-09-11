package wechat

import (
	"errors"

	"glex/internal/infra/pay/adapter"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
)

// Factory is the wechat channel's adapter factory.
type Factory struct{}

// NewFactory creates the wechat adapter factory.
func NewFactory() *Factory { return &Factory{} }

// Schema exposes the channel's config schema (ADR 0021).
func (f *Factory) Schema() payentity.ChannelSchema { return ConfigSchema() }

// errGatewayNotImplemented stands in until the gateway lands with its
// contract tests (ticket 03) — schema-first, as the design orders it.
var errGatewayNotImplemented = errors.New("wechat: gateway not implemented yet")

// NewGateway builds a gateway from a validated config value set.
func (f *Factory) NewGateway(cfg map[string]string, opts ...adapter.GatewayOption) (payservice.ChannelGateway, error) {
	return newGateway(cfg, opts...)
}
