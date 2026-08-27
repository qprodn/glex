// Package lock defines the distributed-lock contract (ADR 0003): one
// closure-style interface, the sentinel errors callers switch on, and the
// per-call options. Application modules depend on this package only; the
// Redis implementation lives in internal/infra/locker and is bound via fx.
package lock

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v3/errors"
)

// ErrLockBusy is returned when the lock is held by someone else — the
// fail-fast default, or an exhausted WithWait budget. Kratos maps it to
// HTTP 409, which is the agreed user-visible behaviour for duplicate
// submits (true idempotency is a separate idempotency-key concern).
var ErrLockBusy = errors.Conflict("LOCK_BUSY", "operation already in progress")

// ErrLockLost is returned when the lock was definitively lost while the
// critical section ran: the section was cancelled and the whole operation
// should be retried, never resumed. Kratos maps it to HTTP 500.
var ErrLockLost = errors.InternalServer("LOCK_LOST", "distributed lock lost during operation")

// Locker runs fn under a distributed lock. It mirrors Transaction.InTx:
// acquire, run, release — with renewal and loss handling hidden inside.
type Locker interface {
	// key follows module:resource[:id] ("access:menu-sync", "user:42");
	// the implementation adds the glex:lock: prefix. fn receives a context
	// that is cancelled when the lock is lost. Nesting the same key in the
	// same goroutine deadlocks: reentrant locking is not supported.
	WithLock(ctx context.Context, key string, fn func(ctx context.Context) error, opts ...Option) error
}

// Config carries the resolved options; its fields are read by the
// implementation package.
type Config struct {
	// TTL is the lock's time-to-live, renewed every TTL/3 while fn runs.
	TTL time.Duration
	// Wait bounds how long acquisition retries before ErrLockBusy;
	// zero means fail-fast (the default).
	Wait time.Duration
	// Fixed disables renewal: the lock expires after TTL as-is and the
	// caller accepts that the critical section may outlive it.
	Fixed bool
}

// DefaultConfig returns the hardcoded defaults (ADR 0003): 30s TTL,
// fail-fast acquisition, renewal on.
func DefaultConfig() Config { return Config{TTL: 30 * time.Second} }

// Option customises a single WithLock call.
type Option func(*Config)

// WithTTL overrides the lock TTL.
func WithTTL(d time.Duration) Option { return func(c *Config) { c.TTL = d } }

// WithWait retries acquisition for up to d before returning ErrLockBusy.
func WithWait(d time.Duration) Option { return func(c *Config) { c.Wait = d } }

// WithFixedTTL disables the renewal watchdog.
func WithFixedTTL() Option { return func(c *Config) { c.Fixed = true } }
