package jobinfra

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "time/tzdata" // embed the tz database: scratch containers carry none

	"glex/internal/conf"
	"glex/internal/domain/job"
	jobentity "glex/internal/domain/job/entity"
	jobservice "glex/internal/domain/job/service"
	"glex/internal/lock"

	"go.uber.org/fx"
)

// SchedulerRuntime is the built-in trigger adapter (ADR 0023): a symmetric
// tick loop per replica. Each tick loads the enabled set, computes due cron
// occurrences, and dispatches each due fire under its per-occurrence Redis
// claim — another replica winning the claim, or a still-running previous
// fire, skips the occurrence. It is the deliberately replaceable file of the
// job module: an external scheduler takes over by flipping conf.Job.enabled
// off and triggering through Fire semantics instead.
type SchedulerRuntime struct {
	jobs     jobservice.Jobs
	locker   lock.Locker
	logger   *slog.Logger
	interval time.Duration
	location *time.Location
	now      func() time.Time

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// Built-in defaults when the config section is absent or zero-valued
// (ADR 0023; the ratelimit config's 0-means-default pattern).
const (
	defaultTickSeconds    = 10
	defaultJobTimezone    = "Asia/Shanghai"
	fireClaimTTLSlack     = 60 * time.Second
)

// NewSchedulerRuntime builds the runtime and owns its lifecycle, the
// eventbus NewBus pattern: the loop starts on the fx start hook; Stop
// cancels the loop context — in-flight fires see the cancellation, must
// return like everything else in the service, and still land their log rows
// (Fire writes on a detached context) — then waits for them. An invalid
// timezone fails construction: replicas must agree on the zone, so a bad
// one is a deployment error, not a warning.
func NewSchedulerRuntime(lc fx.Lifecycle, jobs jobservice.Jobs, locker lock.Locker, cfg *conf.Job, logger *slog.Logger) (*SchedulerRuntime, error) {
	tick := time.Duration(cfg.GetTickSeconds()) * time.Second
	if tick <= 0 {
		tick = defaultTickSeconds * time.Second
	}
	zone := cfg.GetTimezone()
	if zone == "" {
		zone = defaultJobTimezone
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("job: unknown timezone %q: %w", zone, err)
	}
	rt := &SchedulerRuntime{
		jobs:     jobs,
		locker:   locker,
		logger:   logger.With(slog.String("component", "infra.job.runtime")),
		interval: tick,
		location: location,
		now:      time.Now,
	}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if !cfg.GetEnabled() {
				rt.logger.Info("scheduler loop disabled by config; jobs fire only through manual runs",
					slog.String("operation", "job.rt.start"))
				return nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			rt.cancel = cancel
			rt.wg.Add(1)
			go rt.loop(ctx)
			rt.logger.Info("scheduler loop started",
				slog.String("operation", "job.rt.start"),
				slog.Duration("interval", rt.interval),
				slog.String("location", rt.location.String()))
			return nil
		},
		OnStop: func(context.Context) error {
			if rt.cancel != nil {
				rt.cancel()
				rt.wg.Wait()
			}
			return nil
		},
	})
	return rt, nil
}

// loop is the tick driver; lastFired (job id → last dispatched occurrence)
// is process-local memory of what this replica already attempted.
func (r *SchedulerRuntime) loop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	lastFired := make(map[int64]time.Time)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx, lastFired)
		}
	}
}

// tick scans the enabled set and dispatches every due occurrence.
func (r *SchedulerRuntime) tick(ctx context.Context, lastFired map[int64]time.Time) {
	jobs, err := r.jobs.ListEnabled(ctx)
	if err != nil {
		// Scan failure — Redis/DB down — pauses firing on this replica for
		// one tick; the next tick retries (ADR 0023's no-degraded-mode
		// stance).
		r.logger.Warn("scheduler scan failed",
			slog.String("operation", "job.rt.scan"),
			slog.Any("error", err))
		return
	}
	now := r.now().In(r.location)
	for _, j := range jobs {
		r.dispatchDue(ctx, j, now, lastFired)
	}
}

// dispatchDue computes the occurrence to fire for one job, if any. The
// basis is the later of the stored row's updated_at (an edit re-anchors the
// schedule) and this replica's last dispatched occurrence; occurrences
// older than the fire grace (2× the tick interval — downtime, a stalled
// tick) are skipped permanently, never backfilled (ADR 0023). lastFired is
// marked before dispatch so an overlapping tick cannot double-send the same
// occurrence.
func (r *SchedulerRuntime) dispatchDue(ctx context.Context, j *jobentity.Job, now time.Time, lastFired map[int64]time.Time) {
	sched, err := j.Schedule()
	if err != nil {
		// Save-time validation keeps stored cron parseable; reaching here
		// means drift (an old row edited by hand). Say so and stand down.
		r.logger.Warn("stored cron no longer parses; job stands down until edited",
			slog.String("operation", "job.rt.due"),
			slog.Int64("job_id", j.ID),
			slog.String("cron", j.Cron),
			slog.Any("error", err))
		return
	}
	basis := j.UpdatedAt
	if lf, ok := lastFired[j.ID]; ok && lf.After(basis) {
		basis = lf
	}
	grace := 2 * r.interval
	next := sched.Next(basis.In(r.location))
	for !next.IsZero() && !next.After(now) {
		if now.Sub(next) > grace {
			// Stale occurrence — downtime backlog; skip it permanently.
			lastFired[j.ID] = next
			next = sched.Next(next)
			continue
		}
		lastFired[j.ID] = next
		r.dispatch(ctx, j, next)
		return
	}
}

// dispatch claims the occurrence across replicas (job:fire:<id>:<epoch>,
// ADR 0023 — the run lock alone cannot dedup a fast finisher against a
// late ticker) and fires in the background so one long run never starves
// the loop. Either lock being busy consumes the occurrence quietly.
func (r *SchedulerRuntime) dispatch(ctx context.Context, j *jobentity.Job, occurrence time.Time) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		claim := fmt.Sprintf("job:fire:%d:%d", j.ID, occurrence.Unix())
		ttl := time.Duration(j.TimeoutSeconds)*time.Second + fireClaimTTLSlack
		err := r.locker.WithLock(ctx, claim,
			func(ctx context.Context) error {
				return r.jobs.Fire(ctx, j, jobentity.TriggerScheduled)
			},
			lock.WithTTL(ttl),
		)
		if err == nil {
			return
		}
		if errors.Is(err, lock.ErrLockBusy) || errors.Is(err, job.ErrJobBusy) {
			r.logger.Info("occurrence skipped; another replica claimed it or the previous run is still in flight",
				slog.String("operation", "job.rt.dispatch"),
				slog.Int64("job_id", j.ID),
				slog.Time("occurrence", occurrence))
			return
		}
		r.logger.Warn("scheduled fire failed",
			slog.String("operation", "job.rt.dispatch"),
			slog.Int64("job_id", j.ID),
			slog.Time("occurrence", occurrence),
			slog.Any("error", err))
	}()
}

// RegisterExecutors fills the Registry from the "job-executors" value group
// — the one aggregation point, the eventbus RegisterSubscriptions pattern
// (ADR 0022/0023). Providers' ResultTags and this invoke's ParamTags must
// name the same group.
func RegisterExecutors(executors []job.Executor, registry *job.Registry) error {
	for _, e := range executors {
		registry.Register(e)
	}
	return nil
}
