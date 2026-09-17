package jobinfra

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	jobentity "glex/internal/domain/job/entity"
	jobservice "glex/internal/domain/job/service"
	"glex/internal/lock"
)

// fakeJobsService answers only the two calls the runtime makes; every other
// method of the embedded nil interface panics if reached — a test that
// trips one is a test worth failing loudly.
type fakeJobsService struct {
	jobservice.Jobs
	mu      sync.Mutex
	enabled []*jobentity.Job
	fired   []int64
	fireErr error
}

func (f *fakeJobsService) ListEnabled(context.Context) ([]*jobentity.Job, error) {
	return f.enabled, nil
}

func (f *fakeJobsService) Fire(_ context.Context, j *jobentity.Job, _ jobentity.Trigger) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fired = append(f.fired, j.ID)
	return f.fireErr
}

// recordingLocker adapts lock.Locker and records every requested key.
type recordingLocker struct {
	mu     sync.Mutex
	busyOn map[string]bool
	keys   []string
}

func newRecordingLocker() *recordingLocker {
	return &recordingLocker{busyOn: map[string]bool{}}
}

func (r *recordingLocker) WithLock(_ context.Context, key string, fn func(ctx context.Context) error, _ ...lock.Option) error {
	r.mu.Lock()
	if r.busyOn[key] {
		r.mu.Unlock()
		return lock.ErrLockBusy
	}
	r.keys = append(r.keys, key)
	r.mu.Unlock()
	return fn(context.Background())
}

func (r *recordingLocker) requested() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.keys...)
}

// rtHarness wires a runtime with a fake clock; tests drive tick() directly.
type rtHarness struct {
	rt    *SchedulerRuntime
	svc   *fakeJobsService
	clock *fakeClock
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func newRTHarness(t *testing.T, interval time.Duration, locker *recordingLocker) *rtHarness {
	t.Helper()
	if locker == nil {
		locker = newRecordingLocker()
	}
	svc := &fakeJobsService{}
	clock := &fakeClock{now: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	rt := &SchedulerRuntime{
		jobs:     svc,
		locker:   locker,
		logger:   slog.New(slog.DiscardHandler),
		interval: interval,
		location: time.UTC,
		now:      clock.Now,
	}
	return &rtHarness{rt: rt, svc: svc, clock: clock}
}

func at(h, m, s int) time.Time {
	return time.Date(2026, 9, 17, h, m, s, 0, time.UTC)
}

func jobRow(id int64, cron string, updatedAt time.Time) *jobentity.Job {
	return &jobentity.Job{ID: id, Name: "j", Handler: "h", Cron: cron, Enabled: true, TimeoutSeconds: 30, UpdatedAt: updatedAt}
}

func (h *rtHarness) runTick() {
	lastFired := make(map[int64]time.Time)
	h.rt.tick(context.Background(), lastFired)
	h.rt.wg.Wait() // drain background dispatches
}

func TestTickFiresDueOccurrence(t *testing.T) {
	h := newRTHarness(t, 10*time.Second, nil)
	h.svc.enabled = []*jobentity.Job{jobRow(7, "* * * * *", at(12, 4, 50))}
	h.clock.Set(at(12, 5, 3))
	h.runTick()
	if len(h.svc.fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(h.svc.fired))
	}
}

func TestTickDoesNotRefireSameOccurrence(t *testing.T) {
	h := newRTHarness(t, 10*time.Second, nil)
	h.svc.enabled = []*jobentity.Job{jobRow(7, "* * * * *", at(12, 4, 50))}
	h.clock.Set(at(12, 5, 3))
	// One shared lastFired across ticks is what the loop owns; simulate it.
	lastFired := make(map[int64]time.Time)
	h.rt.tick(context.Background(), lastFired)
	h.rt.wg.Wait()
	h.clock.Set(at(12, 5, 8))
	h.rt.tick(context.Background(), lastFired)
	h.rt.wg.Wait()
	if len(h.svc.fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(h.svc.fired))
	}
	// The next minute's occurrence fires when it comes due.
	h.clock.Set(at(12, 6, 2))
	h.rt.tick(context.Background(), lastFired)
	h.rt.wg.Wait()
	if len(h.svc.fired) != 2 {
		t.Fatalf("fired = %d, want 2", len(h.svc.fired))
	}
}

func TestTickNoBackfillAfterDowntime(t *testing.T) {
	h := newRTHarness(t, 10*time.Second, nil)
	// Hourly job whose schedule anchor is hours in the past — a restart
	// after downtime. No occurrence is within the 20s grace, so nothing
	// may fire now.
	h.svc.enabled = []*jobentity.Job{jobRow(7, "0 * * * *", at(6, 0, 0))}
	h.clock.Set(at(12, 15, 0))
	h.runTick()
	if len(h.svc.fired) != 0 {
		t.Fatalf("backfilled occurrences fired: %d", len(h.svc.fired))
	}
	// The next future occurrence (13:00) fires when it arrives.
	h.clock.Set(at(13, 0, 5))
	h.runTick()
	if len(h.svc.fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(h.svc.fired))
	}
}

func TestTickClaimKeyCarriesEpoch(t *testing.T) {
	locker := newRecordingLocker()
	h := newRTHarness(t, 10*time.Second, locker)
	h.svc.enabled = []*jobentity.Job{jobRow(7, "* * * * *", at(12, 4, 50))}
	h.clock.Set(at(12, 5, 3))
	h.runTick()
	keys := locker.requested()
	want := "job:fire:7:" + strconv.FormatInt(at(12, 5, 0).Unix(), 10)
	if len(keys) != 1 || keys[0] != want {
		t.Fatalf("claims = %v, want [%s]", keys, want)
	}
}

func TestTickSkipsWhenClaimBusy(t *testing.T) {
	locker := newRecordingLocker()
	// Another replica holds the 12:05:00 claim.
	locker.busyOn["job:fire:7:"+strconv.FormatInt(at(12, 5, 0).Unix(), 10)] = true
	h := newRTHarness(t, 10*time.Second, locker)
	h.svc.enabled = []*jobentity.Job{jobRow(7, "* * * * *", at(12, 4, 50))}
	h.clock.Set(at(12, 5, 3))
	lastFired := make(map[int64]time.Time)
	h.rt.tick(context.Background(), lastFired)
	h.rt.wg.Wait()
	if len(h.svc.fired) != 0 {
		t.Fatalf("claimed-by-other occurrence fired locally")
	}
	// The occurrence is consumed: a later tick must not retry it.
	h.clock.Set(at(12, 5, 8))
	h.rt.tick(context.Background(), lastFired)
	h.rt.wg.Wait()
	if len(h.svc.fired) != 0 {
		t.Fatalf("consumed occurrence retried")
	}
}

func TestEditedJobRebasesSchedule(t *testing.T) {
	h := newRTHarness(t, 10*time.Second, nil)
	// lastFired remembers 12:05, but the row was edited at 12:07 — the edit
	// re-anchors; the 12:06 occurrence (before the edit) must not fire.
	lastFired := map[int64]time.Time{7: at(12, 5, 0)}
	h.svc.enabled = []*jobentity.Job{jobRow(7, "* * * * *", at(12, 7, 0))}
	h.clock.Set(at(12, 7, 30))
	h.rt.tick(context.Background(), lastFired)
	h.rt.wg.Wait()
	if len(h.svc.fired) != 0 {
		t.Fatalf("pre-edit occurrence fired after rebase")
	}
	// The first occurrence after the edit (12:08) fires on time.
	h.clock.Set(at(12, 8, 2))
	h.rt.tick(context.Background(), lastFired)
	h.rt.wg.Wait()
	if len(h.svc.fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(h.svc.fired))
	}
}

func TestStoredCronDriftStandsDown(t *testing.T) {
	h := newRTHarness(t, 10*time.Second, nil)
	h.svc.enabled = []*jobentity.Job{jobRow(7, "not a cron", at(12, 4, 50))}
	h.clock.Set(at(12, 5, 3))
	h.runTick() // must not panic
	if len(h.svc.fired) != 0 {
		t.Fatalf("drifted cron fired")
	}
}
