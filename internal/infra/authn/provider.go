package authninfra

import (
	"glex/internal/conf"

	"github.com/sa-tokens/sa-token-go/core"
	"github.com/sa-tokens/sa-token-go/core/adapter"
	"go.uber.org/fx"
)

// Module contains authn persistence, cache, and authentication adapters.
var Module = fx.Module("authn-infra",
	fx.Provide(
		NewAuthStorage,
		provideAuthManager,
		NewLoginSessionRepo,
		NewAuthenticator,
		NewLoginCounterRepo,
		NewLoginChallengeRepo,
		NewAuthLoginBackend,
		NewCredentialRepo,
	),
)

// provideAuthManager registers the sa-token Manager's cleanup as a lifecycle
// hook so it closes on shutdown, before the storage-level Data cleanup.
func provideAuthManager(lc fx.Lifecycle, storage adapter.Storage, c *conf.Auth) (*core.Manager, error) {
	manager, cleanup, err := NewAuthManager(storage, c)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.StopHook(cleanup))
	return manager, nil
}
