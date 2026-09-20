// Package jobinfra hosts the job module's persistence adapters: the Job and
// Job Log repos over ent — and, from the runtime commit on, the scheduler
// loop and the executor registration invoke (ADR 0023).
package jobinfra

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/job"
	jobentity "glex/internal/domain/job/entity"
	jobrepo "glex/internal/domain/job/repository"
	"glex/internal/infra/ent"
	jobtab "glex/internal/infra/ent/job"
	"glex/internal/infra/store"
	"glex/pkg/listq"
)

// toBizJob maps an ent Job PO to a jobentity.Job DO.
func toBizJob(po *ent.Job) *jobentity.Job {
	return &jobentity.Job{
		ID:             po.ID,
		Name:           po.Name,
		Handler:        po.Handler,
		Cron:           po.Cron,
		Args:           po.Args,
		Enabled:        po.Enabled,
		TimeoutSeconds: po.TimeoutSeconds,
		Remark:         po.Remark,
		CreateBy:       po.CreateBy,
		UpdateBy:       po.UpdateBy,
		CreatedAt:      po.CreatedAt,
		UpdatedAt:      po.UpdatedAt,
	}
}

type jobRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewJobRepo creates a new JobRepo instance.
func NewJobRepo(data *store.Data) jobrepo.JobRepo {
	return &jobRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.job.job")),
	}
}

// jobStoreError is the log-and-map for unexpected job-store failures (ADR
// 0007): the raw error is logged once at the handling site with its operation
// and returned as the module's sentinel, so no raw ent error crosses the
// repository boundary. Expected business conditions (ent.IsNotFound and
// friends) map silently at the call sites; a canceled request is benign and
// returns the sentinel without logging.
func jobStoreError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return job.ErrJobStoreUnavailable
	}
	logger.ErrorContext(ctx, "job store operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return job.ErrJobStoreUnavailable
}

func (r *jobRepo) FindByID(ctx context.Context, id int64) (*jobentity.Job, error) {
	po, err := r.data.DB.Job(ctx).Query().
		Where(jobtab.IDEQ(id), jobtab.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, job.ErrJobNotFound
		}
		return nil, jobStoreError(ctx, r.logger, "job.find_by_id", err)
	}
	return toBizJob(po), nil
}

func (r *jobRepo) FindByName(ctx context.Context, name string) (*jobentity.Job, error) {
	po, err := r.data.DB.Job(ctx).Query().
		Where(jobtab.NameEQ(name), jobtab.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, job.ErrJobNotFound
		}
		return nil, jobStoreError(ctx, r.logger, "job.find_by_name", err)
	}
	return toBizJob(po), nil
}

func (r *jobRepo) List(ctx context.Context, q listq.Query) ([]*jobentity.Job, error) {
	pos, err := r.data.DB.Job(ctx).Query().
		Where(
			jobtab.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, jobStoreError(ctx, r.logger, "job.list", err)
	}
	jobs := make([]*jobentity.Job, 0, len(pos))
	for _, po := range pos {
		jobs = append(jobs, toBizJob(po))
	}
	return jobs, nil
}

// ListEnabled walks the whole active enabled set — dozens of rows at back-
// office scale — every scheduler tick (ADR 0023).
func (r *jobRepo) ListEnabled(ctx context.Context) ([]*jobentity.Job, error) {
	pos, err := r.data.DB.Job(ctx).Query().
		Where(jobtab.DeletedAtIsNil(), jobtab.EnabledEQ(true)).
		Order(ent.Asc(jobtab.FieldID)).
		All(ctx)
	if err != nil {
		return nil, jobStoreError(ctx, r.logger, "job.list_enabled", err)
	}
	jobs := make([]*jobentity.Job, 0, len(pos))
	for _, po := range pos {
		jobs = append(jobs, toBizJob(po))
	}
	return jobs, nil
}

// CreateJob inserts the job row; the partial unique index on name is the
// race-proof backstop behind the service's friendly pre-check. A constraint
// hit is that backstop firing — an expected business condition mapped
// silently to the module's conflict sentinel (ADR 0007), never a 503.
func (r *jobRepo) Create(ctx context.Context, j *jobentity.Job) (*jobentity.Job, error) {
	po, err := r.data.DB.Job(ctx).Create().
		SetID(newSnowflakeID()).
		SetName(j.Name).
		SetHandler(j.Handler).
		SetCron(j.Cron).
		SetArgs(j.Args).
		SetEnabled(j.Enabled).
		SetTimeoutSeconds(j.TimeoutSeconds).
		SetRemark(j.Remark).
		SetCreateBy(j.CreateBy).
		SetUpdateBy(j.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, job.ErrJobNameConflict
		}
		return nil, jobStoreError(ctx, r.logger, "job.create", err)
	}
	return toBizJob(po), nil
}

// UpdateJob applies the updatable fields; enabled moves through SetEnabled,
// never here, so the pause switch cannot ride along a content edit. A name
// collision caught by the partial unique index maps to the conflict
// sentinel, the create path's stance.
func (r *jobRepo) Update(ctx context.Context, j *jobentity.Job) (*jobentity.Job, error) {
	po, err := r.data.DB.Job(ctx).UpdateOneID(j.ID).
		SetName(j.Name).
		SetHandler(j.Handler).
		SetCron(j.Cron).
		SetArgs(j.Args).
		SetTimeoutSeconds(j.TimeoutSeconds).
		SetRemark(j.Remark).
		SetUpdateBy(j.UpdateBy).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, job.ErrJobNotFound
		}
		if ent.IsConstraintError(err) {
			return nil, job.ErrJobNameConflict
		}
		return nil, jobStoreError(ctx, r.logger, "job.update", err)
	}
	return toBizJob(po), nil
}

func (r *jobRepo) SetEnabled(ctx context.Context, req jobrepo.SetEnabledRequest) error {
	err := r.data.DB.Job(ctx).UpdateOneID(req.ID).
		SetEnabled(req.Enabled).
		SetUpdateBy(req.ActorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return job.ErrJobNotFound
		}
		return jobStoreError(ctx, r.logger, "job.set_enabled", err)
	}
	return nil
}

// DeleteJob soft-deletes the job (ADR 0002 — no row is hard-deleted),
// stamping req.ActorID into the audit fields.
func (r *jobRepo) Delete(ctx context.Context, req jobrepo.DeleteRequest) error {
	err := r.data.DB.Job(ctx).UpdateOneID(req.ID).
		SetDeletedAt(time.Now()).
		SetUpdateBy(req.ActorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return job.ErrJobNotFound
		}
		return jobStoreError(ctx, r.logger, "job.delete", err)
	}
	return nil
}
