// Package param publishes the param module's cross-module contracts in the
// anti-corruption shape of ADR 0005: contract.go holds the consumer-shaped
// interface, impl/ adapts the provider's domain capability, and parammock
// holds the mockgen-generated test double. Contracts are consumed by
// constructor injection into the calling application service; cmd stays the
// only composition root.
package param

import (
	"context"
)

//go:generate go tool mockgen -package parammock -destination parammock/config_reader.go glex/internal/crossdomain/param ConfigReader

// ConfigReader reads Runtime Parameter values by key — the only way another
// module consumes a parameter. It is deliberately in-process only: RuoYi's
// by-key HTTP endpoint let any logged-in user read any value (initial
// passwords included), and glex has no pre-login consumer that would need
// one (docs/modules/param.md). A missing key returns the param domain's
// CONFIG_NOT_FOUND; a returned value is a plain string whose parsing is the
// consumer's job — tolerate dirty values and fail safe.
type ConfigReader interface {
	ValueByKey(ctx context.Context, key string) (string, error)
}
