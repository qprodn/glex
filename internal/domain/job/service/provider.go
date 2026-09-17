package service

import (
	"glex/internal/domain/job"

	"go.uber.org/fx"
)

// Module contains the job domain service and the executor registry it (and
// the scheduler's registration invoke) share — one Registry instance per
// process, filled at fx start from the "job-executors" value group.
var Module = fx.Module("job-domain-service",
	fx.Provide(
		job.NewRegistry,
		fx.Annotate(NewJobsService, fx.As(new(Jobs))),
	),
)
