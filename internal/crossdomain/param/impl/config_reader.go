package impl

import (
	"context"

	"glex/internal/crossdomain/param"
	paramservice "glex/internal/domain/param/service"
)

// configReader satisfies the param consumption contract by delegating to the
// provider's param domain service: the by-key read lives there, beside the
// key-uniqueness rules. v1 reads hit the indexed table directly — no cache
// (docs/modules/param.md); if one is ever warranted it goes inside this
// adapter or the repo, never into the contract.
type configReader struct {
	configs paramservice.Configs
}

// NewConfigReader adapts the param domain service to the crossdomain
// consumption contract.
func NewConfigReader(configs paramservice.Configs) param.ConfigReader {
	return &configReader{configs: configs}
}

// ValueByKey returns the parameter's value. Errors cross with the param
// domain's kratos reasons (CONFIG_NOT_FOUND for a missing or withdrawn key);
// the domain service logged its breadcrumb.
func (r *configReader) ValueByKey(ctx context.Context, key string) (string, error) {
	c, err := r.configs.FindByKey(ctx, key)
	if err != nil {
		return "", err
	}
	return c.ConfigValue, nil
}
