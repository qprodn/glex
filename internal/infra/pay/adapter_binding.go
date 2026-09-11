package payinfra

import (
	payservice "glex/internal/domain/pay/service"
	"glex/internal/infra/pay/adapter"
	"glex/internal/infra/pay/adapter/alipay"
	"glex/internal/infra/pay/adapter/wechat"
	"glex/internal/infra/pay/crypt"
)

// NewAdapterRegistry builds the channel registry over every adopted
// channel's factory. Adding a channel = one adapter package + one line
// here.
func NewAdapterRegistry() *adapter.Registry {
	r := adapter.NewRegistry()
	r.Register(wechat.NewFactory())
	r.Register(alipay.NewFactory())
	return r
}

// newCatalog adapts the registry to the ChannelCatalog port.
func newCatalog(r *adapter.Registry) payservice.ChannelCatalog { return r }

// newGatewayRegistry adapts the registry to the GatewayRegistry port.
func newGatewayRegistry(r *adapter.Registry) payservice.GatewayRegistry { return r }

// newConfigCipher adapts the credential cipher to the ConfigCipher port
// (ADR 0018).
func newConfigCipher(c *crypt.Cipher) payservice.ConfigCipher { return c }
