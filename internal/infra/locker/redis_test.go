package locker

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"

	"glex/internal/lock"
)

// The RedisLocker satisfies the shared contract structurally; the fx.As
// binding in provider.go relies on exactly this.
var _ lock.Locker = (*RedisLocker)(nil)

func newTestLocker(t *testing.T) (*RedisLocker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &RedisLocker{client: redislock.New(rdb), logger: slog.Default()}, mr
}

// holdKey seeds a foreign holder for key, returning its release func.
func holdKey(t *testing.T, l *RedisLocker, key string, ttl time.Duration) func() {
	t.Helper()
	lk, err := l.client.Obtain(context.Background(), keyPrefix+key, ttl, nil)
	if err != nil {
		t.Fatalf("seed hold on %q: %v", key, err)
	}
	return func() { _ = lk.Release(context.Background()) }
}

func TestWithLockRunsSectionAndReleasesKey(t *testing.T) {
	l, mr := newTestLocker(t)
	errSentinel := errors.New("section failed")

	err := l.WithLock(context.Background(), "access:menu-sync", func(ctx context.Context) error {
		if !mr.Exists(keyPrefix + "access:menu-sync") {
			t.Error("lock key missing while the section is running")
		}
		return errSentinel
	})
	if !errors.Is(err, errSentinel) {
		t.Fatalf("WithLock: got %v, want the section's error", err)
	}
	if mr.Exists(keyPrefix + "access:menu-sync") {
		t.Error("lock key still present after WithLock returned")
	}
}

func TestWithLockFailsFastWhileHeldAndRetakesAfterRelease(t *testing.T) {
	l, _ := newTestLocker(t)
	release := holdKey(t, l, "user:42", time.Minute)
	t.Cleanup(release)

	err := l.WithLock(context.Background(), "user:42", func(ctx context.Context) error {
		t.Error("section must not run while the lock is held elsewhere")
		return nil
	})
	if !errors.Is(err, lock.ErrLockBusy) {
		t.Fatalf("WithLock while held: got %v, want ErrLockBusy", err)
	}

	release()
	if err := l.WithLock(context.Background(), "user:42", func(ctx context.Context) error { return nil }); err != nil {
		t.Fatalf("WithLock after release: %v", err)
	}
}

func TestWithWaitAcquiresOnceHolderReleases(t *testing.T) {
	l, _ := newTestLocker(t)
	release := holdKey(t, l, "user:42", time.Minute)
	go func() {
		time.Sleep(150 * time.Millisecond)
		release()
	}()

	err := l.WithLock(context.Background(), "user:42", func(ctx context.Context) error { return nil },
		lock.WithWait(2*time.Second))
	if err != nil {
		t.Fatalf("WithLock with WithWait: %v", err)
	}
}

func TestWithWaitReturnsBusyWhenBudgetExhausts(t *testing.T) {
	l, _ := newTestLocker(t)
	t.Cleanup(holdKey(t, l, "user:42", time.Minute))

	start := time.Now()
	err := l.WithLock(context.Background(), "user:42", func(ctx context.Context) error { return nil },
		lock.WithWait(300*time.Millisecond))
	elapsed := time.Since(start)
	if !errors.Is(err, lock.ErrLockBusy) {
		t.Fatalf("WithLock after exhausted wait: got %v, want ErrLockBusy", err)
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("returned after %v; the wait budget was not honoured", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("returned after %v; the wait budget overshot", elapsed)
	}
}

func TestWatchdogRenewsLockPastItsTTL(t *testing.T) {
	l, mr := newTestLocker(t)
	done := make(chan error, 1)
	go func() {
		done <- l.WithLock(context.Background(), "access:menu-sync", func(ctx context.Context) error {
			time.Sleep(1200 * time.Millisecond) // 2x the TTL below
			return nil
		}, lock.WithTTL(600*time.Millisecond))
	}()

	time.Sleep(200 * time.Millisecond)
	if !mr.Exists(keyPrefix + "access:menu-sync") {
		t.Fatal("lock key missing early in the section")
	}
	// Without renewal the 600ms TTL would have expired the key by now.
	time.Sleep(600 * time.Millisecond)
	if !mr.Exists(keyPrefix + "access:menu-sync") {
		t.Fatal("lock key expired mid-section; the watchdog did not renew it")
	}
	if err := <-done; err != nil {
		t.Fatalf("WithLock with renewal: %v", err)
	}
}

func TestFixedTTLLockExpiresWhileSectionRuns(t *testing.T) {
	l, mr := newTestLocker(t)

	err := l.WithLock(context.Background(), "access:menu-sync", func(ctx context.Context) error {
		time.Sleep(500 * time.Millisecond) // past the fixed TTL
		return nil
	}, lock.WithTTL(200*time.Millisecond), lock.WithFixedTTL())
	if err != nil {
		t.Fatalf("WithLock with fixed TTL: %v", err)
	}
	mr.FastForward(time.Second)
	if mr.Exists(keyPrefix + "access:menu-sync") {
		t.Error("fixed-TTL lock outlived its TTL; nothing should have renewed it")
	}
}

func TestStolenLockCancelsSectionAndReportsLost(t *testing.T) {
	l, mr := newTestLocker(t)

	err := l.WithLock(context.Background(), "authn:thief", func(ctx context.Context) error {
		mr.Del(keyPrefix + "authn:thief") // steal: the token no longer matches
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
			t.Error("section was never cancelled after the lock was stolen")
			return nil
		}
	}, lock.WithTTL(300*time.Millisecond))
	if !errors.Is(err, lock.ErrLockLost) {
		t.Fatalf("WithLock with stolen lock: got %v, want ErrLockLost", err)
	}
}

func TestNestedDifferentKeysSucceed(t *testing.T) {
	l, _ := newTestLocker(t)

	err := l.WithLock(context.Background(), "access:outer", func(ctx context.Context) error {
		return l.WithLock(ctx, "authn:inner", func(ctx context.Context) error { return nil })
	})
	if err != nil {
		t.Fatalf("nested WithLock on different keys: %v", err)
	}
}

func TestPanicInSectionStillReleasesLock(t *testing.T) {
	l, mr := newTestLocker(t)

	func() {
		defer func() { _ = recover() }()
		_ = l.WithLock(context.Background(), "authn:panic", func(ctx context.Context) error {
			panic("boom")
		})
	}()
	if mr.Exists(keyPrefix + "authn:panic") {
		t.Error("lock key still present after the section panicked")
	}
}

func TestCallerContextCancelStillReleasesLock(t *testing.T) {
	l, mr := newTestLocker(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := l.WithLock(ctx, "user:42", func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock with cancelled caller: %v", err)
	}
	if mr.Exists(keyPrefix + "user:42") {
		t.Error("release must outlive the caller's cancelled context")
	}
}
