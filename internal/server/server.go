package server

import "go.uber.org/fx"

// Module is server providers.
var Module = fx.Provide(NewGRPCServer, NewHTTPServer)
