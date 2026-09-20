// Package service hosts the job module's domain rules (ADR 0005): job CRUD
// validation against the live executor registry, and Fire — the one run
// path both the scheduler loop and manual run-now flow through.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"glex/internal/domain/job"
	jobentity "glex/internal/domain/job/entity"
	"glex/internal/domain/job/repository"
	"glex/internal/lock"
	"glex/pkg/listq"
	"glex/pkg/logx"
)

// Jobs is the job domain service behind the job endpoints and the scheduler
// loop: the only path to Scheduled Job reads and writes, the Fire
// bookkeeping, and the Job Log reads (ADR 0005). Every error a method
// returns to its caller first logs its Warn breadcrumb (ADR 0011).
type Jobs interface {
	Get(ctx context.Context, id int64) (*jobentity.Job, error)
	List(ctx context.Context, q listq.Query) ([]*jobentity.Job, error)
	// ListEnabled returns every active enabled job — the scheduler loop's
	// scan read (ADR 0023).
	ListEnabled(ctx context.Context) ([]*jobentity.Job, error)
	// Create validates the job — including cron parse and handler residency
	// against the live registry — and persists it.
	Create(ctx context.Context, j *jobentity.Job) (*jobentity.Job, error)
	// Update validates the change and persists it. The name-uniqueness
	// pre-check excludes the row itself.
	Update(ctx context.Context, j *jobentity.Job) (*jobentity.Job, error)
	// SetEnabled flips the pause/resume switch. It never touches content
	// fields, so the switch cannot ride along an edit.
	SetEnabled(ctx context.Context, req SetEnabledRequest) error
	// Delete soft-deletes the job; an in-flight run finishes, no new firing
	// happens once the scheduler scan stops seeing the row.
	Delete(ctx context.Context, req DeleteRequest) error
	// ListExecutors returns the registered executor names — the save-time
	// validation's registry, surfaced for the console's handler picker.
	ListExecutors() []string

	// Fire runs one firing of the job end to end: under the per-job run
	// lock (no overlap, ADR 0023), with the job's timeout, recovering a
	// panicking executor, and recording the completed run as a Job Log row.
	// It does not check enabled — the scheduler fires only enabled rows,
	// manual run-now is allowed on paused jobs by design. Busy returns
	// ErrJobBusy. A definitively lost run lock (renewal failure, ADR 0003)
	// returns lock.ErrLockLost verbatim after the interrupted run's failure
	// row has landed — the lock contract's own sentinel, the same
	// pass-through pay's application layer practices. Everything else
	// either lands a log row or reports the failure to record one.
	Fire(ctx context.Context, req FireRequest) error

	GetLog(ctx context.Context, id int64) (*jobentity.JobLog, error)
	ListLogs(ctx context.Context, q listq.Query) ([]*jobentity.JobLog, error)
	// DeleteLog soft-deletes one log row — history cleanup, never record
	// editing (ADR 0023).
	DeleteLog(ctx context.Context, req DeleteRequest) error
}

// SetEnabledRequest is one pause/resume flip: the job id, the switch's
// target state, and the actor stamped into the audit fields.
type SetEnabledRequest struct {
	ID      int64
	Enabled bool
	ActorID int64
}

// DeleteRequest carries one withdrawal (ADR 0002): the record id and the
// actor stamped into the audit fields. One shape serves the job and log
// deletes — both are the same withdrawal.
type DeleteRequest struct {
	ID      int64
	ActorID int64
}

// FireRequest is one firing: the job to run and the trigger that caused
// it — the scheduler rides TriggerScheduled, manual run-now
// TriggerManual.
type FireRequest struct {
	Job     *jobentity.Job
	Trigger jobentity.Trigger
}

type jobsImpl struct {
	jobs      repository.JobRepo
	logs      repository.JobLogRepo
	executors *job.Registry
	locker    lock.Locker
	logger    *slog.Logger
}

// NewJobsService creates the job domain service. The Locker lives here —
// not only in the application layer as pay does — because Fire is the
// shared choke point of the scheduler path and the manual path, and the
// no-overlap invariant must hold for both (ADR 0023).
func NewJobsService(jobs repository.JobRepo, logs repository.JobLogRepo, executors *job.Registry, locker lock.Locker, logger *slog.Logger) Jobs {
	return &jobsImpl{
		jobs:      jobs,
		logs:      logs,
		executors: executors,
		locker:    locker,
		logger:    logger.With(slog.String("component", "domain.job.svc")),
	}
}

// --- Scheduled Job (定时任务) ---

func (s *jobsImpl) Get(ctx context.Context, id int64) (*jobentity.Job, error) {
	j, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.get", err)
		return nil, err
	}
	return j, nil
}

func (s *jobsImpl) List(ctx context.Context, q listq.Query) ([]*jobentity.Job, error) {
	js, err := s.jobs.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.list", err)
		return nil, err
	}
	return js, nil
}

func (s *jobsImpl) ListEnabled(ctx context.Context) ([]*jobentity.Job, error) {
	js, err := s.jobs.ListEnabled(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.list_enabled", err)
		return nil, err
	}
	return js, nil
}

func (s *jobsImpl) Create(ctx context.Context, j *jobentity.Job) (*jobentity.Job, error) {
	if err := s.validate(ctx, j, 0); err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.create", err)
		return nil, err
	}
	created, err := s.jobs.Create(ctx, j)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.create", err)
		return nil, err
	}
	return created, nil
}

func (s *jobsImpl) Update(ctx context.Context, j *jobentity.Job) (*jobentity.Job, error) {
	if _, err := s.jobs.FindByID(ctx, j.ID); err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.update", err)
		return nil, err
	}
	if err := s.validate(ctx, j, j.ID); err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.update", err)
		return nil, err
	}
	updated, err := s.jobs.Update(ctx, j)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.update", err)
		return nil, err
	}
	return updated, nil
}

func (s *jobsImpl) SetEnabled(ctx context.Context, req SetEnabledRequest) error {
	if err := s.jobs.SetEnabled(ctx, repository.SetEnabledRequest{ID: req.ID, Enabled: req.Enabled, ActorID: req.ActorID}); err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.set_enabled", err)
		return err
	}
	return nil
}

func (s *jobsImpl) Delete(ctx context.Context, req DeleteRequest) error {
	if err := s.jobs.Delete(ctx, repository.DeleteRequest{ID: req.ID, ActorID: req.ActorID}); err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.delete", err)
		return err
	}
	return nil
}

func (s *jobsImpl) ListExecutors() []string {
	return s.executors.Names()
}

// --- Fire (一次执行) ---

// Fire runs the job under the per-job run lock. The lock's TTL covers the
// run timeout plus slack; the renewal watchdog (ADR 0003) keeps it alive
// for long runs and cancels this context if the lock is definitively lost.
func (s *jobsImpl) Fire(ctx context.Context, req FireRequest) error {
	startedAt := time.Now()
	err := s.locker.WithLock(ctx, job.RunLockKey(req.Job.ID),
		func(ctx context.Context) error {
			return s.runAndRecord(ctx, req, startedAt)
		},
		lock.WithTTL(time.Duration(req.Job.TimeoutSeconds)*time.Second+30*time.Second),
	)
	if err != nil {
		if errors.Is(err, lock.ErrLockBusy) {
			err = job.ErrJobBusy
		}
		logx.Propagate(ctx, s.logger, "job.svc.fire", err)
		return err
	}
	return nil
}

// runAndRecord executes the firing and lands its Job Log row. The log write
// runs on a detached, timeout-bounded context — the record must survive the
// run's own cancellation (timeout, lock loss), the audit streams' detached
// insert pattern (ADR 0012).
func (s *jobsImpl) runAndRecord(ctx context.Context, req FireRequest, startedAt time.Time) error {
	j := req.Job
	executor := s.executors.Lookup(j.Handler)
	entry := &jobentity.JobLog{
		JobID:     j.ID,
		Handler:   j.Handler,
		Trigger:   req.Trigger,
		StartedAt: startedAt,
	}
	switch {
	case executor == nil:
		// Code/config drift made visible, not an error to the caller: the
		// fire happened, its outcome is the failure row (ADR 0023).
		entry.Status = jobentity.RunFailure
		entry.Message = truncateMessage("handler not registered")
	default:
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(j.TimeoutSeconds)*time.Second)
		summary, runErr := s.execute(runCtx, executor, j)
		cancel()
		if runErr != nil {
			entry.Status = jobentity.RunFailure
			entry.Message = truncateMessage(runErr.Error())
		} else {
			entry.Status = jobentity.RunSuccess
			entry.Message = truncateMessage(summary)
		}
	}
	entry.DurationMs = time.Since(startedAt).Milliseconds()

	logCtx, cancelLog := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancelLog()
	if _, err := s.logs.Create(logCtx, entry); err != nil {
		// The run happened but its record did not. Fire's method boundary
		// owns this breadcrumb (ADR 0011) — propagating here too would log
		// the same failure twice.
		return fmt.Errorf("record job log: %w", err)
	}
	return nil
}

// execute runs the executor, converting a panic into a failure result — a
// panicking executor must not take the scheduler goroutine down (the
// eventbus memory backend's isolation stance, ADR 0022).
func (s *jobsImpl) execute(ctx context.Context, executor job.Executor, j *jobentity.Job) (summary string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("executor panicked: %v", r)
		}
	}()
	return executor.Run(ctx, j.Args)
}

// truncateMessage bounds what a run contributes to its log row — by runes,
// never bytes: byte slicing can split a multi-byte UTF-8 sequence mid-rune,
// and PostgreSQL rejects the invalid bytes on insert, losing the completed
// run's record over a truncation detail (pay's truncateUTF8 precedent).
func truncateMessage(msg string) string {
	runes := []rune(msg)
	if len(runes) <= jobentity.MaxMessageLen {
		return msg
	}
	return string(runes[:jobentity.MaxMessageLen])
}

// --- Job Log (任务日志) ---

func (s *jobsImpl) GetLog(ctx context.Context, id int64) (*jobentity.JobLog, error) {
	l, err := s.logs.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.get_log", err)
		return nil, err
	}
	return l, nil
}

func (s *jobsImpl) ListLogs(ctx context.Context, q listq.Query) ([]*jobentity.JobLog, error) {
	ls, err := s.logs.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.list_logs", err)
		return nil, err
	}
	return ls, nil
}

func (s *jobsImpl) DeleteLog(ctx context.Context, req DeleteRequest) error {
	if err := s.logs.Delete(ctx, repository.DeleteRequest{ID: req.ID, ActorID: req.ActorID}); err != nil {
		logx.Propagate(ctx, s.logger, "job.svc.delete_log", err)
		return err
	}
	return nil
}

// validate enforces the save-time invariants: shape (name, timeout), cron
// parse, handler residency, and name uniqueness among active rows
// (excludeID = 0 = create). The partial unique index remains the race-proof
// last line of defense. Its caller's method boundary owns the breadcrumb
// (ADR 0011).
func (s *jobsImpl) validate(ctx context.Context, j *jobentity.Job, excludeID int64) error {
	if err := j.Validate(); err != nil {
		return err
	}
	if len(j.Handler) == 0 || len(j.Handler) > jobentity.MaxHandlerLen {
		return job.ErrJobInvalid
	}
	if len(j.Cron) == 0 || len(j.Cron) > jobentity.MaxCronLen {
		return job.ErrJobCronInvalid
	}
	if _, err := j.Schedule(); err != nil {
		return job.ErrJobCronInvalid
	}
	if len(j.Args) > jobentity.MaxArgsLen {
		return job.ErrJobInvalid
	}
	if s.executors.Lookup(j.Handler) == nil {
		return job.ErrJobHandlerUnknown
	}
	return s.assertNameAvailable(ctx, j.Name, excludeID)
}

// assertNameAvailable checks the name is not taken by another active job.
// A not-found from the repo is the expected "available" case.
func (s *jobsImpl) assertNameAvailable(ctx context.Context, name string, excludeID int64) error {
	existing, err := s.jobs.FindByName(ctx, name)
	if err != nil {
		if errors.Is(err, job.ErrJobNotFound) {
			return nil
		}
		return err
	}
	if existing.ID != excludeID {
		return job.ErrJobNameConflict
	}
	return nil
}
