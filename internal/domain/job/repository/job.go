package repository

import (
	"context"

	"glex/internal/domain/job/entity"
	"glex/pkg/listq"
)

// JobRepo is the repository interface for Scheduled Jobs (the inversion
// seam declared in domain, implemented in infra).
type JobRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.Job, error)
	// FindByName resolves the display-name uniqueness pre-check.
	FindByName(ctx context.Context, name string) (*entity.Job, error)
	List(ctx context.Context, q listq.Query) ([]*entity.Job, error)
	// ListEnabled returns every active enabled job — the scheduler loop's
	// scan read. A dedicated method rather than a filter keeps that path
	// free of AIP-160 parsing on every tick.
	ListEnabled(ctx context.Context) ([]*entity.Job, error)
	Create(ctx context.Context, j *entity.Job) (*entity.Job, error)
	// Update applies the updatable fields: name, handler, cron, args,
	// timeout, remark. The partial unique index on name is the race-proof
	// backstop behind the service's friendly pre-check.
	Update(ctx context.Context, j *entity.Job) (*entity.Job, error)
	// SetEnabled flips the schedule on (resume) or off (pause), stamping
	// req.ActorID into the audit fields.
	SetEnabled(ctx context.Context, req SetEnabledRequest) error
	// Delete soft-deletes the job, stamping req.ActorID into the audit
	// fields (ADR 0002 — no row is hard-deleted). In-flight runs finish; no
	// new firing happens once the scan stops seeing the row.
	Delete(ctx context.Context, req DeleteRequest) error
}

// SetEnabledRequest is one pause/resume flip: the job id, the switch's
// target state, and the actor stamped into the audit fields.
type SetEnabledRequest struct {
	ID      int64
	Enabled bool
	ActorID int64
}

// DeleteRequest carries one withdrawal (ADR 0002): the record id and the
// actor stamped into the audit fields. One shape serves the Job and Job
// Log repos — both deletes are the same withdrawal.
type DeleteRequest struct {
	ID      int64
	ActorID int64
}

// JobLogRepo is the repository interface for Job Logs.
type JobLogRepo interface {
	FindByID(ctx context.Context, id int64) (*entity.JobLog, error)
	List(ctx context.Context, q listq.Query) ([]*entity.JobLog, error)
	// Create records one completed firing. It is the only write a log row
	// ever takes — there is no update path (ADR 0023).
	Create(ctx context.Context, l *entity.JobLog) (*entity.JobLog, error)
	// Delete soft-deletes the log row (withdrawal — history cleanup, not
	// record editing), stamping req.ActorID into the audit fields.
	Delete(ctx context.Context, req DeleteRequest) error
}
