package locker

import (
	"glex/internal/lock"

	"go.uber.org/fx"
)

// Module wires the Redis locker behind the shared lock.Locker contract.
var Module = fx.Options(
	fx.Provide(NewRedisLocker),
	fx.Provide(fx.Annotate(NewRedisLocker, fx.As(new(lock.Locker)))),
)
