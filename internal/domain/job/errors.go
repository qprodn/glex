// Package job owns the job module per ADR 0005: Scheduled Jobs and their
// Job Logs — the back-office-configured recurring firings of code-registered
// Job Executors. The package root holds the typed errors and the Executor
// contract; the objects live in entity, the ports in repository, and the
// module's own rules in service.
package job

import (
	"github.com/go-kratos/kratos/v3/errors"
)

var (
	// ErrJobNotFound: the requested job does not exist (or is soft-deleted).
	ErrJobNotFound = errors.NotFound("JOB_NOT_FOUND", "job not found")
	// ErrJobNameConflict: the name already exists among active jobs.
	ErrJobNameConflict = errors.Conflict("JOB_NAME_CONFLICT", "job name already exists")
	// ErrJobInvalid: name is blank or too long, or timeout_seconds is outside
	// 1–3600.
	ErrJobInvalid = errors.BadRequest("JOB_INVALID", "name is required and timeout_seconds must be within 1-3600")
	// ErrJobCronInvalid: the cron field is not a valid 5-field expression.
	ErrJobCronInvalid = errors.BadRequest("JOB_CRON_INVALID", "cron must be a valid 5-field expression")
	// ErrJobHandlerUnknown: the handler names no registered Job Executor —
	// checked at save time so a typo cannot arm a job that can never fire
	// (ADR 0023).
	ErrJobHandlerUnknown = errors.BadRequest("JOB_HANDLER_UNKNOWN", "handler is not a registered executor")
	// ErrJobBusy: a run of this job is still in flight — the per-job run
	// lock is held. Kratos maps it to HTTP 409, the same user-visible shape
	// as LOCK_BUSY.
	ErrJobBusy = errors.Conflict("JOB_BUSY", "job run already in progress")
)

var (
	// ErrJobLogNotFound: the requested job log does not exist (or is
	// soft-deleted).
	ErrJobLogNotFound = errors.NotFound("JOB_LOG_NOT_FOUND", "job log not found")
)

var (
	// ErrJobStoreUnavailable: a job-store operation failed unexpectedly —
	// driver down, tx failure, serialization. Adapters log the raw error once
	// and return this sentinel (ADR 0007), so no driver text crosses the
	// repository boundary; clients see 503, never a driver message.
	ErrJobStoreUnavailable = errors.ServiceUnavailable("JOB_STORE_UNAVAILABLE", "job store unavailable")
)
