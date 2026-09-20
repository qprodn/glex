// Package job hosts the job module's application use cases (ADR 0005):
// the Scheduled Job console surface and the manual run-now. All repository
// access goes through the job domain service; writes are single-row
// statements, so the module declares no Transaction (the file module's
// precedent).
package job

import (
	"context"
	"errors"
	"log/slog"
	"time"

	jobv1 "glex/api/job/v1"
	"glex/internal/bizpkg"
	"glex/internal/conf"
	"glex/internal/domain/job"
	jobentity "glex/internal/domain/job/entity"
	jobservice "glex/internal/domain/job/service"
	"glex/internal/lock"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// defaultTimeoutSeconds backs conf.Job.default_timeout_seconds = 0.
const defaultTimeoutSeconds = 60

// runDispatchSlack bounds the detached manual-run context beyond the job's
// own timeout — Fire enforces the real deadline; the slack only keeps the
// dispatch context alive through the log write that follows a timed-out
// run.
const runDispatchSlack = 30 * time.Second

// convertJob maps a jobentity.Job DO to the proto Job DTO. enabled is
// optional on the wire (create distinguishes unset from an explicit
// pause); every stored row has a definite value, so the pointer is always
// set on the way out.
func convertJob(j *jobentity.Job) *jobv1.Job {
	enabled := j.Enabled
	return &jobv1.Job{
		JobId:          j.ID,
		Name:           j.Name,
		Handler:        j.Handler,
		Cron:           j.Cron,
		Args:           j.Args,
		Enabled:        &enabled,
		TimeoutSeconds: j.TimeoutSeconds,
		Remark:         j.Remark,
		CreateBy:       j.CreateBy,
		UpdateBy:       j.UpdateBy,
		CreatedAt:      timestamppb.New(j.CreatedAt),
		UpdatedAt:      timestamppb.New(j.UpdatedAt),
	}
}

// convertJobLog maps a jobentity.JobLog DO to the proto JobLog DTO.
func convertJobLog(l *jobentity.JobLog) *jobv1.JobLog {
	return &jobv1.JobLog{
		JobLogId:   l.ID,
		JobId:      l.JobID,
		Handler:    l.Handler,
		Trigger:    triggerToProto(l.Trigger),
		Status:     runStatusToProto(l.Status),
		Message:    l.Message,
		StartedAt:  timestamppb.New(l.StartedAt),
		DurationMs: l.DurationMs,
		CreateBy:   l.CreateBy,
		UpdateBy:   l.UpdateBy,
		CreatedAt:  timestamppb.New(l.CreatedAt),
		UpdatedAt:  timestamppb.New(l.UpdatedAt),
	}
}

func triggerToProto(t jobentity.Trigger) jobv1.Trigger {
	switch t {
	case jobentity.TriggerScheduled:
		return jobv1.Trigger_TRIGGER_SCHEDULED
	case jobentity.TriggerManual:
		return jobv1.Trigger_TRIGGER_MANUAL
	case jobentity.TriggerExternal:
		return jobv1.Trigger_TRIGGER_EXTERNAL
	default:
		return jobv1.Trigger_TRIGGER_UNSPECIFIED
	}
}

func runStatusToProto(s jobentity.RunStatus) jobv1.RunStatus {
	switch s {
	case jobentity.RunSuccess:
		return jobv1.RunStatus_RUN_SUCCESS
	default:
		return jobv1.RunStatus_RUN_FAILURE
	}
}

// JobApplication implements the scheduled-task management service. All
// repository access goes through the job domain service, where the module's
// rules live (ADR 0005). Authentication and the operation's Functional
// Permission policy are enforced by the registered HTTP middleware.
type JobApplication struct {
	jobv1.UnimplementedJobServiceServer

	svc    jobservice.Jobs
	locker lock.Locker
	cfg    *conf.Job
	logger *slog.Logger
}

// NewJobApplication creates a JobApplication. locker serves only the
// manual-run busy probe — the run itself locks inside the domain service's
// Fire, the single choke point of scheduled and manual paths (ADR 0023).
func NewJobApplication(svc jobservice.Jobs, locker lock.Locker, cfg *conf.Job, logger *slog.Logger) *JobApplication {
	return &JobApplication{
		svc:    svc,
		locker: locker,
		cfg:    cfg,
		logger: logger.With(slog.String("component", "application.job")),
	}
}

// defaultTimeout resolves the create-time timeout default: the row's value
// is stamped at create so later config changes leave existing jobs
// explicit (ADR 0023).
func (s *JobApplication) defaultTimeout() int32 {
	if v := s.cfg.GetDefaultTimeoutSeconds(); v > 0 {
		return int32(v)
	}
	return defaultTimeoutSeconds
}

// CreateJob creates a job; handler residency and cron parse are validated
// by the domain service. An unset enabled (the optional marker) is stamped
// true — a client that leaves the switch out means "fire by default", not
// "pause silently" (ADR 0023's console contract).
func (s *JobApplication) CreateJob(ctx context.Context, req *jobv1.CreateJobRequest) (*jobv1.Job, error) {
	in := req.GetJob()
	timeout := in.GetTimeoutSeconds()
	if timeout == 0 {
		timeout = s.defaultTimeout()
	}
	enabled := true
	if in.Enabled != nil {
		enabled = in.GetEnabled()
	}
	j := &jobentity.Job{
		Name:           in.GetName(),
		Handler:        in.GetHandler(),
		Cron:           in.GetCron(),
		Args:           in.GetArgs(),
		Enabled:        enabled,
		TimeoutSeconds: timeout,
		Remark:         in.GetRemark(),
	}
	actor := bizpkg.AuditActor(ctx)
	j.CreateBy, j.UpdateBy = actor, actor
	created, err := s.svc.Create(ctx, j)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.create_job", err)
		return nil, err
	}
	return convertJob(created), nil
}

// GetJob retrieves a job by id.
func (s *JobApplication) GetJob(ctx context.Context, req *jobv1.GetJobRequest) (*jobv1.Job, error) {
	j, err := s.svc.Get(ctx, req.GetJobId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.get_job", err)
		return nil, err
	}
	return convertJob(j), nil
}

// jobListSpec is this module's list fact (ADR 0024).
var jobListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("name", filtering.TypeString),
		filtering.DeclareIdent("handler", filtering.TypeString),
		filtering.DeclareIdent("enabled", filtering.TypeBool),
	},
	DefaultOrder: []ordering.Field{{Path: "id"}},
}

// jobLogListSpec is this module's list fact (ADR 0024).
var jobLogListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("job_id", filtering.TypeInt),
		filtering.DeclareIdent("handler", filtering.TypeString),
		filtering.DeclareIdent("trigger", filtering.TypeInt),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("created_at", filtering.TypeTimestamp),
	},
	DefaultOrder: []ordering.Field{{Path: "created_at", Desc: true}},
}

// ListJobs lists jobs with AIP-160 filter, AIP-132 order, and page-token
// pagination.
func (s *JobApplication) ListJobs(ctx context.Context, req *jobv1.ListJobsRequest) (*jobv1.JobSet, error) {
	q, page, err := listq.Parse(req, jobListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.list_jobs", err)
		return nil, err
	}
	jobs, err := s.svc.List(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.list_jobs", err)
		return nil, err
	}
	set := &jobv1.JobSet{Jobs: make([]*jobv1.Job, 0, len(jobs))}
	for _, j := range jobs {
		set.Jobs = append(set.Jobs, convertJob(j))
	}
	set.NextPageToken = page.NextToken(len(jobs))
	return set, nil
}

// UpdateJob partially updates a job via field-mask; every write revalidates
// handler residency and the cron expression. enabled never moves here —
// SetJobEnabled owns the switch.
func (s *JobApplication) UpdateJob(ctx context.Context, req *jobv1.UpdateJobRequest) (*jobv1.Job, error) {
	current, err := s.GetJob(ctx, &jobv1.GetJobRequest{JobId: req.GetJob().GetJobId()})
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.update_job", err)
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetJob())
	j := &jobentity.Job{
		ID:             current.GetJobId(),
		Name:           current.GetName(),
		Handler:        current.GetHandler(),
		Cron:           current.GetCron(),
		Args:           current.GetArgs(),
		TimeoutSeconds: current.GetTimeoutSeconds(),
		Remark:         current.GetRemark(),
		UpdateBy:       bizpkg.AuditActor(ctx),
	}
	updated, err := s.svc.Update(ctx, j)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.update_job", err)
		return nil, err
	}
	return convertJob(updated), nil
}

// DeleteJob soft-deletes a job by id.
func (s *JobApplication) DeleteJob(ctx context.Context, req *jobv1.DeleteJobRequest) (*emptypb.Empty, error) {
	if err := s.svc.Delete(ctx, req.GetJobId(), bizpkg.AuditActor(ctx)); err != nil {
		logx.Propagate(ctx, s.logger, "job.delete_job", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// SetJobEnabled resumes or pauses a job's schedule.
func (s *JobApplication) SetJobEnabled(ctx context.Context, req *jobv1.SetJobEnabledRequest) (*jobv1.Job, error) {
	if err := s.svc.SetEnabled(ctx, req.GetJobId(), req.GetEnabled(), bizpkg.AuditActor(ctx)); err != nil {
		logx.Propagate(ctx, s.logger, "job.set_job_enabled", err)
		return nil, err
	}
	return s.GetJob(ctx, &jobv1.GetJobRequest{JobId: req.GetJobId()})
}

// RunJob fires one manual run now, asynchronously: a fail-fast probe of the
// run lock surfaces JOB_BUSY (409) synchronously, then the run dispatches
// on a detached, timeout-bounded context and this call returns immediately
// — the outcome lands as a Job Log row (ADR 0023). Allowed on paused jobs;
// a scheduled fire slipping between probe and dispatch is absorbed by
// Fire's own lock as a skipped manual run, Warn-logged there.
func (s *JobApplication) RunJob(ctx context.Context, req *jobv1.RunJobRequest) (*emptypb.Empty, error) {
	j, err := s.svc.Get(ctx, req.GetJobId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.run_job", err)
		return nil, err
	}
	if err := s.locker.WithLock(ctx, job.RunLockKey(j.ID), func(context.Context) error { return nil }); err != nil {
		if errors.Is(err, lock.ErrLockBusy) {
			err = job.ErrJobBusy
		}
		logx.Propagate(ctx, s.logger, "job.run_job", err)
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx),
		time.Duration(j.TimeoutSeconds)*time.Second+runDispatchSlack)
	go func() {
		defer cancel()
		if err := s.svc.Fire(runCtx, j, jobentity.TriggerManual); err != nil {
			// Degraded, not propagated — the requester is long gone; the
			// operator sees the outcome (or its absence) in the log list.
			s.logger.Warn("manual run failed to complete",
				slog.String("operation", "job.run_job"),
				slog.Int64("job_id", j.ID),
				slog.Any("error", err))
		}
	}()
	return &emptypb.Empty{}, nil
}

// ListJobLogs lists job logs with AIP filter/order/page-token, newest
// first by default.
func (s *JobApplication) ListJobLogs(ctx context.Context, req *jobv1.ListJobLogsRequest) (*jobv1.JobLogSet, error) {
	q, page, err := listq.Parse(req, jobLogListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.list_job_logs", err)
		return nil, err
	}
	logs, err := s.svc.ListLogs(ctx, q)
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.list_job_logs", err)
		return nil, err
	}
	set := &jobv1.JobLogSet{JobLogs: make([]*jobv1.JobLog, 0, len(logs))}
	for _, l := range logs {
		set.JobLogs = append(set.JobLogs, convertJobLog(l))
	}
	set.NextPageToken = page.NextToken(len(logs))
	return set, nil
}

// GetJobLog retrieves one log row by id.
func (s *JobApplication) GetJobLog(ctx context.Context, req *jobv1.GetJobLogRequest) (*jobv1.JobLog, error) {
	l, err := s.svc.GetLog(ctx, req.GetJobLogId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "job.get_job_log", err)
		return nil, err
	}
	return convertJobLog(l), nil
}

// DeleteJobLog withdraws one log row.
func (s *JobApplication) DeleteJobLog(ctx context.Context, req *jobv1.DeleteJobLogRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteLog(ctx, req.GetJobLogId(), bizpkg.AuditActor(ctx)); err != nil {
		logx.Propagate(ctx, s.logger, "job.delete_job_log", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListJobExecutors returns the registered executor names, sorted.
func (s *JobApplication) ListJobExecutors(context.Context, *jobv1.ListJobExecutorsRequest) (*jobv1.ExecutorSet, error) {
	names := s.svc.ListExecutors()
	set := &jobv1.ExecutorSet{Executors: make([]*jobv1.Executor, 0, len(names))}
	for _, name := range names {
		set.Executors = append(set.Executors, &jobv1.Executor{Name: name})
	}
	return set, nil
}
