package locker

import (
	"glex/internal/lock"

	"go.uber.org/fx"
)

// Module wires the Redis locker behind the shared lock.Locker contract.
// Nothing consumes the concrete *RedisLocker, so only the interface is
// provided.
var Module = fx.Module("locker",
	fx.Provide(fx.Annotate(NewRedisLocker, fx.As(new(lock.Locker)))),
)
