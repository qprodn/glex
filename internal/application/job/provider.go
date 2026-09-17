package job

import (
	"go.uber.org/fx"
)

// Module contains the job application service.
var Module = fx.Module("job-app",
	fx.Provide(
		NewJobApplication,
	),
)
