package store

import "github.com/google/wire"

// ProviderSet contains shared PostgreSQL, Redis, and transaction providers.
var ProviderSet = wire.NewSet(NewData, NewTxManager)
