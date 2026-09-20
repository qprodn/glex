package job

import (
	"context"
	"fmt"
	"sort"
)

// Executor is one named unit of scheduled work (任务执行器, ADR 0023) — the
// durable asset of the job module. Executors are registered in code by the
// module that owns the work (via the "job-executors" fx value group) and
// referenced by name from Scheduled Job rows; a job row never carries
// executable strings, so no reflection on operator input ever happens. The
// Name/Run shape doubles as the binding shape for external schedulers
// (SnailJob/XXL-Job name their handlers the same way), which is what keeps
// the external replacement path executor-neutral (ADR 0023).
type Executor interface {
	// Name is the registry key a Scheduled Job's handler references. It is
	// stable for the executor's lifetime: renaming is withdraw-and-recreate
	// for every job row using it.
	Name() string
	// Run performs one firing. The context carries the run timeout and dies
	// with the run lock (ADR 0003), so Run must honor cancellation. Firing
	// is best-effort like eventbus delivery (ADR 0022): Run must tolerate
	// the same firing twice and must tolerate a firing never happening. The
	// returned summary lands verbatim (truncated) on the Job Log row; an
	// error marks the run failed and the error text becomes the message.
	Run(ctx context.Context, args string) (string, error)
}

// Registry is the process's Executor table: filled once at fx start from the
// job-executors value group, read afterwards by the domain service (fire,
// save-time handler validation) and the executor listing endpoint. It lives
// at the package root — like internal/lock and internal/eventbus contracts —
// so infra adapters (e.g. pay's sweep executor) implement it without
// importing the domain service package.
type Registry struct {
	executors map[string]Executor
}

// RunLockKey is the per-job no-overlap lock key (ADR 0003 key convention,
// ADR 0023 semantics). The domain service's Fire holds it for every run;
// the application's manual-run probe acquires it fail-fast to surface
// JOB_BUSY synchronously. One owner of the format, two callers.
func RunLockKey(id int64) string {
	return fmt.Sprintf("job:run:%d", id)
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{executors: make(map[string]Executor)}
}

// Register adds one executor. A duplicate name is a wiring bug — the process
// must not silently keep an arbitrary one — so it panics, the same stance
// eventbus takes on a failing subscription registration (ADR 0022).
func (r *Registry) Register(e Executor) {
	if _, dup := r.executors[e.Name()]; dup {
		panic(fmt.Sprintf("job: duplicate executor name %q", e.Name()))
	}
	r.executors[e.Name()] = e
}

// Lookup resolves a handler name; nil means no such executor (ADR 0025 —
// missing is nil, never a bool flag).
func (r *Registry) Lookup(name string) Executor {
	return r.executors[name]
}

// Names returns every registered executor name, sorted — the stable order
// behind the executor listing read.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.executors))
	for name := range r.executors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
