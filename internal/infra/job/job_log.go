package jobinfra

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-kratos/aip-go/ents"
	"glex/internal/domain/job"
	jobentity "glex/internal/domain/job/entity"
	jobrepo "glex/internal/domain/job/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/joblog"
	"glex/internal/infra/store"
	"glex/pkg/listq"
)

// toBizJobLog maps an ent JobLog PO to a jobentity.JobLog DO.
func toBizJobLog(po *ent.JobLog) *jobentity.JobLog {
	return &jobentity.JobLog{
		ID:         po.ID,
		JobID:      po.JobID,
		Handler:    po.Handler,
		Trigger:    jobentity.Trigger(po.Trigger),
		Status:     jobentity.RunStatus(po.Status),
		Message:    po.Message,
		StartedAt:  po.StartedAt,
		DurationMs: po.DurationMs,
		CreateBy:   po.CreateBy,
		UpdateBy:   po.UpdateBy,
		CreatedAt:  po.CreatedAt,
		UpdatedAt:  po.UpdatedAt,
	}
}

type jobLogRepo struct {
	data   *store.Data
	logger *slog.Logger
}

// NewJobLogRepo creates a new JobLogRepo instance.
func NewJobLogRepo(data *store.Data) jobrepo.JobLogRepo {
	return &jobLogRepo{
		data:   data,
		logger: data.Logger.With(slog.String("component", "infra.job.job_log")),
	}
}

func (r *jobLogRepo) FindByID(ctx context.Context, id int64) (*jobentity.JobLog, error) {
	po, err := r.data.DB.JobLog(ctx).Query().
		Where(joblog.IDEQ(id), joblog.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, job.ErrJobLogNotFound
		}
		return nil, jobStoreError(ctx, r.logger, "job.log.find_by_id", err)
	}
	return toBizJobLog(po), nil
}

func (r *jobLogRepo) List(ctx context.Context, q listq.Query) ([]*jobentity.JobLog, error) {
	pos, err := r.data.DB.JobLog(ctx).Query().
		Where(
			joblog.DeletedAtIsNil(),
			ents.ApplyFilter(q.Filter),
		).
		Order(ents.ApplyOrderBy(q.OrderBy)).
		Offset(q.Offset).
		Limit(q.Limit).
		All(ctx)
	if err != nil {
		return nil, jobStoreError(ctx, r.logger, "job.log.list", err)
	}
	logs := make([]*jobentity.JobLog, 0, len(pos))
	for _, po := range pos {
		logs = append(logs, toBizJobLog(po))
	}
	return logs, nil
}

// CreateJobLog records one completed firing — the only write a log row ever
// takes (ADR 0023).
func (r *jobLogRepo) Create(ctx context.Context, l *jobentity.JobLog) (*jobentity.JobLog, error) {
	po, err := r.data.DB.JobLog(ctx).Create().
		SetID(newSnowflakeID()).
		SetJobID(l.JobID).
		SetHandler(l.Handler).
		SetTrigger(int16(l.Trigger)).
		SetStatus(int16(l.Status)).
		SetMessage(l.Message).
		SetStartedAt(l.StartedAt).
		SetDurationMs(l.DurationMs).
		SetCreateBy(l.CreateBy).
		SetUpdateBy(l.UpdateBy).
		Save(ctx)
	if err != nil {
		return nil, jobStoreError(ctx, r.logger, "job.log.create", err)
	}
	return toBizJobLog(po), nil
}

// DeleteJobLog soft-deletes the log row — history cleanup, not record
// editing (ADR 0002) — stamping req.ActorID into the audit fields.
func (r *jobLogRepo) Delete(ctx context.Context, req jobrepo.DeleteRequest) error {
	err := r.data.DB.JobLog(ctx).UpdateOneID(req.ID).
		SetDeletedAt(time.Now()).
		SetUpdateBy(req.ActorID).
		Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return job.ErrJobLogNotFound
		}
		return jobStoreError(ctx, r.logger, "job.log.delete", err)
	}
	return nil
}
