package jobinfra

import (
	"go.uber.org/fx"
)

// Module contains the job module's persistence adapters: the Job and Job Log
// repos. The scheduler runtime joins this module with the runtime commit,
// alongside the executor registration invoke that fills the Registry from
// the "job-executors" value group (ADR 0023).
var Module = fx.Module("job-infra",
	fx.Provide(
		NewJobRepo,
		NewJobLogRepo,
	),
)
