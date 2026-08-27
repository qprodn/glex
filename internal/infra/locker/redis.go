// Package locker implements lock.Locker on Redis using bsm/redislock
// primitives (random token, SET NX PX, Lua compare-and-delete) and adds the
// semantics ADR 0003 settled: fail-fast acquisition, a TTL/3 renewal
// watchdog, and lock-loss cancellation of the critical section.
package locker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/bsm/redislock"

	"glex/internal/infra/store"
	"glex/internal/lock"
)

const (
	// keyPrefix namespaces every lock key, mirroring the glex:auth namespace.
	keyPrefix = "glex:lock:"
	// acquireInterval paces WithWait retries.
	acquireInterval = 100 * time.Millisecond
	// opTimeout bounds release and renew round-trips. The shared Redis
	// client already carries config-driven read/write timeouts; this is only
	// a safety net for calls made after the caller's context is gone.
	opTimeout = time.Second
)

// RedisLocker is the lock.Locker implementation over the shared Redis client.
type RedisLocker struct {
	client *redislock.Client
	logger *slog.Logger
}

// NewRedisLocker builds the locker from the shared store clients.
func NewRedisLocker(data *store.Data, logger *slog.Logger) *RedisLocker {
	return &RedisLocker{
		client: redislock.New(data.Redis),
		logger: logger.With(slog.String("component", "infra.locker")),
	}
}

// WithLock acquires the lock, runs fn, and releases it on return or panic.
// While fn is in flight the lock is renewed every TTL/3; if renewal reports
// the lock definitively lost, fn's context is cancelled and ErrLockLost is
// returned once fn unwinds. Transient renewal errors only retry.
func (l *RedisLocker) WithLock(ctx context.Context, key string, fn func(ctx context.Context) error, opts ...lock.Option) error {
	cfg := lock.DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	// fn runs under a derived context so lock loss can cancel it without
	// touching the caller's.
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lk, err := l.obtain(fctx, key, cfg)
	if err != nil {
		return err
	}

	var lost atomic.Bool
	if !cfg.Fixed {
		go l.renew(fctx, lk, cfg.TTL, &lost, cancel)
	}
	// Defers run LIFO: release the lock while the renewer is still alive,
	// then cancel to stop it.
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), opTimeout)
		defer releaseCancel()
		if err := lk.Release(releaseCtx); err != nil && !errors.Is(err, redislock.ErrLockNotHeld) {
			l.logger.Warn("release lock", slog.String("key", key), slog.Any("error", err))
		}
	}()

	err = fn(fctx)
	if lost.Load() {
		return lock.ErrLockLost
	}
	return err
}

// obtain wraps redislock.Obtain. Zero options means a single attempt — the
// fail-fast default; WithWait turns into a deadline-capped backoff loop.
// redislock joins the context error into its return value when the deadline
// hits mid-retry, so the Wait budget maps to ErrLockBusy as long as the
// caller's own context is still alive.
func (l *RedisLocker) obtain(ctx context.Context, key string, cfg lock.Config) (*redislock.Lock, error) {
	options := &redislock.Options{}
	obtainCtx := ctx
	if cfg.Wait > 0 {
		options.RetryStrategy = redislock.LimitRetry(
			redislock.LinearBackoff(acquireInterval),
			int(cfg.Wait/acquireInterval)+1,
		)
		var cancelObtain context.CancelFunc
		obtainCtx, cancelObtain = context.WithTimeout(ctx, cfg.Wait)
		defer cancelObtain()
	}
	lk, err := l.client.Obtain(obtainCtx, keyPrefix+key, cfg.TTL, options)
	if errors.Is(err, redislock.ErrNotObtained) {
		return nil, lock.ErrLockBusy
	}
	if cfg.Wait > 0 && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return nil, lock.ErrLockBusy
	}
	if err != nil {
		return nil, fmt.Errorf("obtain lock %q: %w", key, err)
	}
	return lk, nil
}

// renew refreshes the lock every ttl/3 until ctx is done. Refresh reports a
// definitively lost lock (stolen or expired — the Lua script no longer
// matches our token) as ErrNotObtained, unlike Release's ErrLockNotHeld; on
// that the critical section is cancelled and marked so WithLock can
// translate the outcome to ErrLockLost. Anything else is treated as
// transient and retried on the next tick.
func (l *RedisLocker) renew(ctx context.Context, lk *redislock.Lock, ttl time.Duration, lost *atomic.Bool, cancel context.CancelFunc) {
	ticker := time.NewTicker(ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, renewCancel := context.WithTimeout(context.WithoutCancel(ctx), opTimeout)
			err := lk.Refresh(renewCtx, ttl, nil)
			renewCancel()
			if err == nil {
				continue
			}
			if errors.Is(err, redislock.ErrNotObtained) {
				l.logger.Warn("lock lost during critical section", slog.Any("error", err))
				lost.Store(true)
				cancel()
				return
			}
			l.logger.Warn("renew lock", slog.Any("error", err))
		}
	}
}
