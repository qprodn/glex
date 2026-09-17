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
	fx.Invoke(
		fx.Annotate(RegisterExecutors, fx.ParamTags(`group:"job-executors"`)),
		anchorRuntime,
	),
)

// anchorRuntime forces fx to construct the SchedulerRuntime so its
// lifecycle hook registers. fx builds only what something depends on, and
// the runtime deliberately has no dependents — without this invoke the
// loop silently never starts (found by live smoke: the hook is the side
// effect, the type is the anchor).
func anchorRuntime(*SchedulerRuntime) {}
