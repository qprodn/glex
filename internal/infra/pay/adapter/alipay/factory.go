// Package alipay adapts Alipay's open platform to the pay module's channel
// seam. See schema.go for the config schema and gateway.go for the seam
// implementation.
package alipay

import (
	"errors"

	"glex/internal/infra/pay/adapter"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
)

// Factory is the alipay channel's adapter factory.
type Factory struct{}

// NewFactory creates the alipay adapter factory.
func NewFactory() *Factory { return &Factory{} }

// Schema exposes the channel's config schema (ADR 0021).
func (f *Factory) Schema() payentity.ChannelSchema { return ConfigSchema() }

// errGatewayNotImplemented stands in until the gateway lands with its
// contract tests (ticket 04) — schema-first, as the design orders it.
var errGatewayNotImplemented = errors.New("alipay: gateway not implemented yet")

// NewGateway builds a gateway from a validated config value set.
func (f *Factory) NewGateway(cfg map[string]string, opts ...adapter.GatewayOption) (payservice.ChannelGateway, error) {
	return newGateway(cfg, opts...)
}
