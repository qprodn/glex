package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"

	"glex/internal/domain/job"
	jobentity "glex/internal/domain/job/entity"
	"glex/internal/domain/job/repository"
	"glex/internal/lock"
	"glex/pkg/listq"
)

// --- fakes ---

// fakeJobRepo is an in-memory JobRepo.
type fakeJobRepo struct {
	mu    sync.Mutex
	rows  map[int64]*jobentity.Job
	names map[string]int64 // active name -> id
	next  int64
}

func newFakeJobRepo() *fakeJobRepo {
	return &fakeJobRepo{rows: map[int64]*jobentity.Job{}, names: map[string]int64{}, next: 100}
}

func (f *fakeJobRepo) FindByID(_ context.Context, id int64) (*jobentity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.rows[id]
	if !ok {
		return nil, job.ErrJobNotFound
	}
	cp := *j
	return &cp, nil
}

func (f *fakeJobRepo) FindByName(_ context.Context, name string) (*jobentity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.names[name]
	if !ok {
		return nil, job.ErrJobNotFound
	}
	cp := *f.rows[id]
	return &cp, nil
}

func (f *fakeJobRepo) List(ctx context.Context, q listq.Query) ([]*jobentity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*jobentity.Job, 0, len(f.rows))
	for _, j := range f.rows {
		cp := *j
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeJobRepo) ListEnabled(ctx context.Context) ([]*jobentity.Job, error) {
	return f.List(ctx, listq.Query{})
}

func (f *fakeJobRepo) Create(_ context.Context, j *jobentity.Job) (*jobentity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	cp := *j
	cp.ID = f.next
	f.rows[cp.ID] = &cp
	f.names[cp.Name] = cp.ID
	return &cp, nil
}

func (f *fakeJobRepo) Update(_ context.Context, j *jobentity.Job) (*jobentity.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.rows[j.ID]
	if !ok {
		return nil, job.ErrJobNotFound
	}
	delete(f.names, stored.Name)
	cp := *j
	f.rows[cp.ID] = &cp
	f.names[cp.Name] = cp.ID
	return &cp, nil
}

func (f *fakeJobRepo) SetEnabled(_ context.Context, id int64, enabled bool, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.rows[id]
	if !ok {
		return job.ErrJobNotFound
	}
	j.Enabled = enabled
	return nil
}

func (f *fakeJobRepo) Delete(_ context.Context, id, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.rows[id]
	if !ok {
		return job.ErrJobNotFound
	}
	delete(f.names, j.Name)
	delete(f.rows, id)
	return nil
}

// fakeJobLogRepo records created log rows.
type fakeJobLogRepo struct {
	mu    sync.Mutex
	rows  []*jobentity.JobLog
	next  int64
	fails bool
}

func newFakeJobLogRepo() *fakeJobLogRepo { return &fakeJobLogRepo{next: 500} }

func (f *fakeJobLogRepo) FindByID(_ context.Context, id int64) (*jobentity.JobLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.rows {
		if l.ID == id {
			cp := *l
			return &cp, nil
		}
	}
	return nil, job.ErrJobLogNotFound
}

func (f *fakeJobLogRepo) List(_ context.Context, _ listq.Query) ([]*jobentity.JobLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*jobentity.JobLog, 0, len(f.rows))
	for _, l := range f.rows {
		cp := *l
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeJobLogRepo) Create(_ context.Context, l *jobentity.JobLog) (*jobentity.JobLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fails {
		return nil, job.ErrJobStoreUnavailable
	}
	f.next++
	cp := *l
	cp.ID = f.next
	f.rows = append(f.rows, &cp)
	return &cp, nil
}

func (f *fakeJobLogRepo) Delete(_ context.Context, id, _ int64) error { return nil }

// fakeLocker adapts lock.Locker; busyOn marks keys that report ErrLockBusy.
type fakeLocker struct {
	mu     sync.Mutex
	busyOn map[string]bool
	held   map[string]bool
}

func newFakeLocker() *fakeLocker {
	return &fakeLocker{busyOn: map[string]bool{}, held: map[string]bool{}}
}

func (f *fakeLocker) WithLock(_ context.Context, key string, fn func(ctx context.Context) error, _ ...lock.Option) error {
	f.mu.Lock()
	if f.busyOn[key] || f.held[key] {
		f.mu.Unlock()
		return lock.ErrLockBusy
	}
	f.held[key] = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.held, key)
		f.mu.Unlock()
	}()
	return fn(context.Background())
}

// stubExecutor is a programmable Job Executor.
type stubExecutor struct {
	name    string
	summary string
	err     error
	panic   bool
	waitCtx bool
	ran     int
	lastArg string
}

func (e *stubExecutor) Name() string { return e.name }

func (e *stubExecutor) Run(ctx context.Context, args string) (string, error) {
	e.ran++
	e.lastArg = args
	if e.panic {
		panic("boom")
	}
	if e.waitCtx {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return e.summary, e.err
}

// --- harness ---

type harness struct {
	svc    Jobs
	jobs   *fakeJobRepo
	logs   *fakeJobLogRepo
	locker *fakeLocker
	exec   *stubExecutor
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	reg := job.NewRegistry()
	exec := &stubExecutor{name: "test.echo", summary: "ok"}
	reg.Register(exec)
	jobs := newFakeJobRepo()
	logs := newFakeJobLogRepo()
	locker := newFakeLocker()
	svc := NewJobsService(jobs, logs, reg, locker, slog.New(slog.DiscardHandler))
	return &harness{svc: svc, jobs: jobs, logs: logs, locker: locker, exec: exec}
}

func validJob() *jobentity.Job {
	return &jobentity.Job{
		Name:           "sweep",
		Handler:        "test.echo",
		Cron:           "*/5 * * * *",
		TimeoutSeconds: 30,
	}
}

// --- CRUD validation ---

func TestCreateJobValidates(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*jobentity.Job)
		want error
	}{
		{"blank name", func(j *jobentity.Job) { j.Name = "" }, job.ErrJobInvalid},
		{"timeout zero", func(j *jobentity.Job) { j.TimeoutSeconds = 0 }, job.ErrJobInvalid},
		{"timeout over cap", func(j *jobentity.Job) { j.TimeoutSeconds = 3601 }, job.ErrJobInvalid},
		{"bad cron", func(j *jobentity.Job) { j.Cron = "not a cron" }, job.ErrJobCronInvalid},
		{"six field cron", func(j *jobentity.Job) { j.Cron = "0 */5 * * * *" }, job.ErrJobCronInvalid},
		{"unknown handler", func(j *jobentity.Job) { j.Handler = "nope" }, job.ErrJobHandlerUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			j := validJob()
			tc.mut(j)
			if _, err := h.svc.Create(context.Background(), j); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if rows, _ := h.jobs.List(context.Background(), listq.Query{}); len(rows) != 0 {
				t.Fatalf("rejected job was persisted")
			}
		})
	}
}

func TestCreateJobNameConflict(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Create(context.Background(), validJob()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := h.svc.Create(context.Background(), validJob()); !errors.Is(err, job.ErrJobNameConflict) {
		t.Fatalf("err = %v, want %v", err, job.ErrJobNameConflict)
	}
}

func TestUpdateJobKeepsOwnName(t *testing.T) {
	h := newHarness(t)
	created, err := h.svc.Create(context.Background(), validJob())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	created.Remark = "renamed content"
	if _, err := h.svc.Update(context.Background(), created); err != nil {
		t.Fatalf("self-name update should pass, got %v", err)
	}
}

func TestUpdateJobNotFound(t *testing.T) {
	h := newHarness(t)
	j := validJob()
	j.ID = 424242
	if _, err := h.svc.Update(context.Background(), j); !errors.Is(err, job.ErrJobNotFound) {
		t.Fatalf("err = %v, want %v", err, job.ErrJobNotFound)
	}
}

func TestListExecutors(t *testing.T) {
	h := newHarness(t)
	got := h.svc.ListExecutors()
	if len(got) != 1 || got[0] != "test.echo" {
		t.Fatalf("executors = %v", got)
	}
}

// --- Fire ---

func TestFireSuccessRecordsLog(t *testing.T) {
	h := newHarness(t)
	created, err := h.svc.Create(context.Background(), validJob())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.svc.Fire(context.Background(), created, jobentity.TriggerScheduled); err != nil {
		t.Fatalf("fire: %v", err)
	}
	rows, _ := h.logs.List(context.Background(), listq.Query{})
	if len(rows) != 1 {
		t.Fatalf("log rows = %d, want 1", len(rows))
	}
	l := rows[0]
	if l.JobID != created.ID || l.Handler != "test.echo" {
		t.Fatalf("log row ids = %+v", l)
	}
	if l.Trigger != jobentity.TriggerScheduled || l.Status != jobentity.RunSuccess || l.Message != "ok" {
		t.Fatalf("log row = trigger:%d status:%d msg:%q", l.Trigger, l.Status, l.Message)
	}
	if l.DurationMs < 0 {
		t.Fatalf("negative duration %d", l.DurationMs)
	}
}

func TestFireExecutorErrorRecordsFailure(t *testing.T) {
	h := newHarness(t)
	h.exec.err = fmt.Errorf("channel down")
	created, _ := h.svc.Create(context.Background(), validJob())
	if err := h.svc.Fire(context.Background(), created, jobentity.TriggerManual); err != nil {
		t.Fatalf("fire reports the run, not the executor error: %v", err)
	}
	rows, _ := h.logs.List(context.Background(), listq.Query{})
	if len(rows) != 1 || rows[0].Status != jobentity.RunFailure || rows[0].Message != "channel down" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Trigger != jobentity.TriggerManual {
		t.Fatalf("trigger = %d", rows[0].Trigger)
	}
}

func TestFirePanicRecordsFailure(t *testing.T) {
	h := newHarness(t)
	h.exec.panic = true
	created, _ := h.svc.Create(context.Background(), validJob())
	if err := h.svc.Fire(context.Background(), created, jobentity.TriggerScheduled); err != nil {
		t.Fatalf("fire: %v", err)
	}
	rows, _ := h.logs.List(context.Background(), listq.Query{})
	if len(rows) != 1 || rows[0].Status != jobentity.RunFailure {
		t.Fatalf("rows = %+v", rows)
	}
	if want := "executor panicked: boom"; rows[0].Message != want {
		t.Fatalf("message = %q, want %q", rows[0].Message, want)
	}
}

func TestFireTimeoutRecordsFailure(t *testing.T) {
	h := newHarness(t)
	h.exec.waitCtx = true
	j := validJob()
	j.TimeoutSeconds = 1 // context.WithTimeout accepts seconds; 1s is tolerable here
	created, _ := h.svc.Create(context.Background(), j)
	start := time.Now()
	if err := h.svc.Fire(context.Background(), created, jobentity.TriggerScheduled); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("run returned too early: %v", elapsed)
	}
	rows, _ := h.logs.List(context.Background(), listq.Query{})
	if len(rows) != 1 || rows[0].Status != jobentity.RunFailure {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Message == "" {
		t.Fatalf("timeout message missing")
	}
}

func TestFireBusyReturnsSentinel(t *testing.T) {
	h := newHarness(t)
	created, _ := h.svc.Create(context.Background(), validJob())
	h.locker.busyOn[fmt.Sprintf("job:run:%d", created.ID)] = true
	if err := h.svc.Fire(context.Background(), created, jobentity.TriggerManual); !errors.Is(err, job.ErrJobBusy) {
		t.Fatalf("err = %v, want %v", err, job.ErrJobBusy)
	}
	if rows, _ := h.logs.List(context.Background(), listq.Query{}); len(rows) != 0 {
		t.Fatalf("busy fire must not record a run")
	}
	if h.exec.ran != 0 {
		t.Fatalf("busy fire must not run the executor")
	}
}

func TestFireUnknownHandlerRecordsDrift(t *testing.T) {
	h := newHarness(t)
	// A job armed by an older deploy's handler: bypass save-time validation
	// by inserting straight through the repo.
	j := validJob()
	j.Handler = "gone.handler"
	stored, err := h.jobs.Create(context.Background(), j)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := h.svc.Fire(context.Background(), stored, jobentity.TriggerScheduled); err != nil {
		t.Fatalf("drift is a recorded outcome, not a fire error: %v", err)
	}
	rows, _ := h.logs.List(context.Background(), listq.Query{})
	if len(rows) != 1 || rows[0].Status != jobentity.RunFailure || rows[0].Handler != "gone.handler" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestFirePassesArgs(t *testing.T) {
	h := newHarness(t)
	j := validJob()
	j.Args = "batch=50"
	created, _ := h.svc.Create(context.Background(), j)
	if err := h.svc.Fire(context.Background(), created, jobentity.TriggerScheduled); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if h.exec.lastArg != "batch=50" {
		t.Fatalf("args = %q", h.exec.lastArg)
	}
}

func TestFireLogWriteFailureSurfaces(t *testing.T) {
	h := newHarness(t)
	h.logs.fails = true
	created, _ := h.svc.Create(context.Background(), validJob())
	if err := h.svc.Fire(context.Background(), created, jobentity.TriggerScheduled); err == nil {
		t.Fatalf("a run whose record failed must surface an error")
	}
}

func TestFireTruncatesLongMessage(t *testing.T) {
	h := newHarness(t)
	h.exec.err = fmt.Errorf("%s", string(make([]byte, jobentity.MaxMessageLen+500)))
	created, _ := h.svc.Create(context.Background(), validJob())
	_ = h.svc.Fire(context.Background(), created, jobentity.TriggerScheduled)
	rows, _ := h.logs.List(context.Background(), listq.Query{})
	if len(rows[0].Message) != jobentity.MaxMessageLen {
		t.Fatalf("message len = %d, want %d", len(rows[0].Message), jobentity.MaxMessageLen)
	}
}

// Compile-time interface checks.
var (
	_ repository.JobRepo    = (*fakeJobRepo)(nil)
	_ repository.JobLogRepo = (*fakeJobLogRepo)(nil)
	_ lock.Locker           = (*fakeLocker)(nil)
	_ job.Executor          = (*stubExecutor)(nil)
	_                       = filtering.Filter{}
	_                       = ordering.OrderBy{}
)

func TestFireTruncatesRunesNotBytes(t *testing.T) {
	h := newHarness(t)
	// 2500 CJK runes = 7500 bytes: byte-slicing at 2000 would split a rune
	// and PostgreSQL would reject the insert; rune-slicing keeps 2000 whole
	// characters.
	h.exec.err = fmt.Errorf("%s", strings.Repeat("支", 2500))
	created, _ := h.svc.Create(context.Background(), validJob())
	_ = h.svc.Fire(context.Background(), created, jobentity.TriggerScheduled)
	rows, _ := h.logs.List(context.Background(), listq.Query{})
	if len(rows) != 1 {
		t.Fatalf("log rows = %d, want 1", len(rows))
	}
	if got := []rune(rows[0].Message); len(got) != jobentity.MaxMessageLen {
		t.Fatalf("runes = %d, want %d", len(got), jobentity.MaxMessageLen)
	}
	if !utf8.ValidString(rows[0].Message) {
		t.Fatalf("truncated message is not valid UTF-8")
	}
}
