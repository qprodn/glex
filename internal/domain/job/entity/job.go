package entity

import (
	"time"

	"github.com/robfig/cron/v3"

	"glex/internal/domain/job"
)

// Job is a Scheduled Job domain object (定时任务, ADR 0023): a
// back-office-configured recurring firing of one Job Executor on a 5-field
// cron schedule. The name is unique among active jobs; the handler names a
// registered executor (validated at save time); timeout bounds each firing.
type Job struct {
	ID             int64
	Name           string
	Handler        string
	Cron           string
	Args           string
	Enabled        bool
	TimeoutSeconds int32
	Remark         string
	CreateBy       int64
	UpdateBy       int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Constraints a Job must satisfy regardless of who supplied it.
const (
	MaxNameLen    = 64
	MaxHandlerLen = 128
	MaxCronLen    = 64
	MaxArgsLen    = 4096
	MinTimeoutSec = 1
	MaxTimeoutSec = 3600
)

// Validate enforces the invariants a Job must satisfy before persistence:
// non-blank bounded name, and timeout within the supported window. Handler
// residency (registry lookup) and cron parsing have their own sentinels and
// live in the domain service, which owns the registry and the clock policy.
func (j *Job) Validate() error {
	if j == nil || j.Name == "" || len(j.Name) > MaxNameLen {
		return job.ErrJobInvalid
	}
	if j.TimeoutSeconds < MinTimeoutSec || j.TimeoutSeconds > MaxTimeoutSec {
		return job.ErrJobInvalid
	}
	return nil
}

// Schedule parses the cron field and returns robfig's schedule. The location
// of every time passed to Next is the caller's concern — the scheduler pins
// one configured timezone for all jobs (ADR 0023); parsing itself is
// location-independent.
func (j *Job) Schedule() (cron.Schedule, error) {
	return cron.ParseStandard(j.Cron)
}

// JobLog is the record of one firing (任务日志, ADR 0023): written once at
// run completion with its outcome, queried and withdrawn, never updated —
// the same write-once spirit as the audit streams (ADR 0012).
type JobLog struct {
	ID         int64
	JobID      int64
	Handler    string
	Trigger    Trigger
	Status     RunStatus
	Message    string
	StartedAt  time.Time
	DurationMs int64
	CreateBy   int64
	UpdateBy   int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Trigger says who asked for the firing.
type Trigger int16

const (
	// TriggerScheduled: the scheduler loop fired the job's cron occurrence.
	TriggerScheduled Trigger = 1
	// TriggerManual: an operator pressed run-now. Allowed regardless of the
	// job's enabled flag — a one-off look is not a schedule change.
	TriggerManual Trigger = 2
	// TriggerExternal is reserved for an external scheduler calling in
	// through the replacement path (ADR 0023); nothing sets it yet.
	TriggerExternal Trigger = 3
)

// RunStatus is one firing's terminal outcome — there is no "running" value:
// the row appears only at completion.
type RunStatus int16

const (
	RunSuccess RunStatus = 1
	RunFailure RunStatus = 2
)

// MaxMessageLen bounds what a run's summary contributes to its Job Log row;
// the writer truncates to it.
const MaxMessageLen = 2000
