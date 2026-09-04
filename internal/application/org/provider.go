package org

import "go.uber.org/fx"

// Module contains the org application providers.
var Module = fx.Module("org-app",
	fx.Provide(
		NewDeptApplication,
	),
)
