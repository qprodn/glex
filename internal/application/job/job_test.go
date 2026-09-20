package job

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	jobv1 "glex/api/job/v1"
	"glex/internal/conf"
	"glex/internal/domain/job"
	jobentity "glex/internal/domain/job/entity"
	jobservice "glex/internal/domain/job/service"
	"glex/internal/lock"

	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fakeJobsSvc answers the application's calls; the embedded nil interface
// panics on anything else.
type fakeJobsSvc struct {
	jobservice.Jobs

	mu       sync.Mutex
	stored   map[int64]*jobentity.Job
	nextID   int64
	created  []*jobentity.Job
	updated  []*jobentity.Job
	enabled  map[int64]bool
	deleted  []int64
	logs     []*jobentity.JobLog
	fired    []int64
	triggers []jobentity.Trigger
	fireErr  error
	fireWG   sync.WaitGroup
}

func newFakeJobsSvc() *fakeJobsSvc {
	return &fakeJobsSvc{stored: map[int64]*jobentity.Job{}, nextID: 100, enabled: map[int64]bool{}}
}

func (f *fakeJobsSvc) Get(_ context.Context, id int64) (*jobentity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.stored[id]
	if !ok {
		return nil, job.ErrJobNotFound
	}
	cp := *j
	return &cp, nil
}

func (f *fakeJobsSvc) Create(_ context.Context, j *jobentity.Job) (*jobentity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	cp := *j
	cp.ID = f.nextID
	f.stored[cp.ID] = &cp
	f.created = append(f.created, &cp)
	return &cp, nil
}

func (f *fakeJobsSvc) Update(_ context.Context, j *jobentity.Job) (*jobentity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.stored[j.ID]; !ok {
		return nil, job.ErrJobNotFound
	}
	cp := *j
	f.stored[j.ID] = &cp
	f.updated = append(f.updated, &cp)
	return &cp, nil
}

func (f *fakeJobsSvc) SetEnabled(_ context.Context, req jobservice.SetEnabledRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.stored[req.ID]; !ok {
		return job.ErrJobNotFound
	}
	f.enabled[req.ID] = req.Enabled
	return nil
}

func (f *fakeJobsSvc) Delete(_ context.Context, req jobservice.DeleteRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.stored, req.ID)
	f.deleted = append(f.deleted, req.ID)
	return nil
}

func (f *fakeJobsSvc) ListExecutors() []string { return []string{"a.first", "b.second"} }

func (f *fakeJobsSvc) Fire(_ context.Context, req jobservice.FireRequest) error {
	f.mu.Lock()
	f.fired = append(f.fired, req.Job.ID)
	f.triggers = append(f.triggers, req.Trigger)
	err := f.fireErr
	f.mu.Unlock()
	f.fireWG.Done()
	return err
}

// probeLocker adapts lock.Locker with a busy switch.
type probeLocker struct {
	busy bool
	keys []string
}

func (p *probeLocker) WithLock(_ context.Context, key string, fn func(ctx context.Context) error, _ ...lock.Option) error {
	p.keys = append(p.keys, key)
	if p.busy {
		return lock.ErrLockBusy
	}
	return fn(context.Background())
}

func newApp(svc *fakeJobsSvc, locker *probeLocker) *JobApplication {
	return NewJobApplication(svc, locker, &conf.Job{}, slog.New(slog.DiscardHandler))
}

func seedJob(id int64) *jobentity.Job {
	return &jobentity.Job{
		ID: id, Name: "sweep", Handler: "pay.expire-sweep", Cron: "* * * * *",
		Enabled: true, TimeoutSeconds: 30, UpdatedAt: time.Now(),
	}
}

func TestCreateJobStampsDefaultTimeout(t *testing.T) {
	svc := newFakeJobsSvc()
	app := newApp(svc, &probeLocker{})
	out, err := app.CreateJob(context.Background(), &jobv1.CreateJobRequest{
		Job: &jobv1.Job{Name: "sweep", Handler: "pay.expire-sweep", Cron: "* * * * *", Enabled: boolPtr(true)},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.GetTimeoutSeconds() != 60 {
		t.Fatalf("timeout = %d, want the built-in default 60", out.GetTimeoutSeconds())
	}
	if len(svc.created) != 1 || svc.created[0].CreateBy != svc.created[0].UpdateBy {
		t.Fatalf("actor stamping missing: %+v", svc.created)
	}
}

func TestCreateJobKeepsStatedTimeout(t *testing.T) {
	svc := newFakeJobsSvc()
	app := newApp(svc, &probeLocker{})
	out, err := app.CreateJob(context.Background(), &jobv1.CreateJobRequest{
		Job: &jobv1.Job{Name: "sweep", Handler: "pay.expire-sweep", Cron: "* * * * *", Enabled: boolPtr(true), TimeoutSeconds: 120},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.GetTimeoutSeconds() != 120 {
		t.Fatalf("timeout = %d, want 120", out.GetTimeoutSeconds())
	}
}

func TestUpdateJobAppliesMask(t *testing.T) {
	svc := newFakeJobsSvc()
	svc.stored[7] = seedJob(7)
	app := newApp(svc, &probeLocker{})
	out, err := app.UpdateJob(context.Background(), &jobv1.UpdateJobRequest{
		Job:        &jobv1.Job{JobId: 7, Name: "renamed", Handler: "pay.expire-sweep", Cron: "0 * * * *", TimeoutSeconds: 90},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name", "timeout_seconds"}},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if out.GetName() != "renamed" || out.GetTimeoutSeconds() != 90 {
		t.Fatalf("masked fields not applied: %+v", out)
	}
	if out.GetCron() != "* * * * *" {
		t.Fatalf("unmasked cron changed: %q", out.GetCron())
	}
}

func TestRunJobDispatchesAsyncManualFire(t *testing.T) {
	svc := newFakeJobsSvc()
	svc.stored[7] = seedJob(7)
	locker := &probeLocker{}
	app := newApp(svc, locker)
	svc.fireWG.Add(1)
	if _, err := app.RunJob(context.Background(), &jobv1.RunJobRequest{JobId: 7}); err != nil {
		t.Fatalf("run: %v", err)
	}
	svc.fireWG.Wait()
	if len(svc.fired) != 1 || svc.fired[0] != 7 {
		t.Fatalf("fired = %v", svc.fired)
	}
	if svc.triggers[0] != jobentity.TriggerManual {
		t.Fatalf("trigger = %v", svc.triggers[0])
	}
	// The busy probe hit the domain-owned run-lock key.
	if len(locker.keys) != 1 || locker.keys[0] != job.RunLockKey(7) {
		t.Fatalf("probe keys = %v", locker.keys)
	}
}

func TestRunJobSurfacesBusySynchronously(t *testing.T) {
	svc := newFakeJobsSvc()
	svc.stored[7] = seedJob(7)
	app := newApp(svc, &probeLocker{busy: true})
	if _, err := app.RunJob(context.Background(), &jobv1.RunJobRequest{JobId: 7}); !errors.Is(err, job.ErrJobBusy) {
		t.Fatalf("err = %v, want JOB_BUSY", err)
	}
	if len(svc.fired) != 0 {
		t.Fatalf("busy run dispatched anyway")
	}
}

func TestRunJobMissingJob(t *testing.T) {
	svc := newFakeJobsSvc()
	app := newApp(svc, &probeLocker{})
	if _, err := app.RunJob(context.Background(), &jobv1.RunJobRequest{JobId: 424242}); !errors.Is(err, job.ErrJobNotFound) {
		t.Fatalf("err = %v, want JOB_NOT_FOUND", err)
	}
}

func TestListJobExecutors(t *testing.T) {
	svc := newFakeJobsSvc()
	app := newApp(svc, &probeLocker{})
	set, err := app.ListJobExecutors(context.Background(), &jobv1.ListJobExecutorsRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(set.GetExecutors()) != 2 || set.GetExecutors()[0].GetName() != "a.first" {
		t.Fatalf("executors = %+v", set.GetExecutors())
	}
}

func TestCreateJobEnabledDefaultsTrue(t *testing.T) {
	svc := newFakeJobsSvc()
	app := newApp(svc, &probeLocker{})
	// enabled deliberately left unset — the optional marker's whole point.
	out, err := app.CreateJob(context.Background(), &jobv1.CreateJobRequest{
		Job: &jobv1.Job{Name: "sweep", Handler: "pay.expire-sweep", Cron: "* * * * *"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !out.GetEnabled() {
		t.Fatalf("unset enabled must stamp true, got paused")
	}
	if svc.created[0].Enabled != true {
		t.Fatalf("persisted enabled = %v, want true", svc.created[0].Enabled)
	}
}

func boolPtr(v bool) *bool { return &v }
