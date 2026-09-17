package jobinfra

import (
	"go.uber.org/fx"
)

// Module contains the job module's persistence adapters and runtime: the
// Job and Job Log repos, the scheduler loop under fx lifecycle, and the
// executor registration invoke that fills the Registry from the
// "job-executors" value group (ADR 0023).
var Module = fx.Module("job-infra",
	fx.Provide(
		NewJobRepo,
		NewJobLogRepo,
		NewSchedulerRuntime,
	),
	fx.Invoke(fx.Annotate(RegisterExecutors, fx.ParamTags(`group:"job-executors"`))),
)
