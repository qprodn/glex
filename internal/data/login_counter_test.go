package data

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"glex/internal/biz"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestLoginCounterRepo(t *testing.T) (biz.LoginCounterRepo, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	data := &Data{rdb: rdb, redisWriteTimeout: time.Second}
	return NewLoginCounterRepo(data, testAuthConf()), mr
}

func TestLoginCountersLockAtConfiguredThresholdsAndExpire(t *testing.T) {
	repo, mr := newTestLoginCounterRepo(t)
	ctx := t.Context()
	for attempt := 1; attempt < 5; attempt++ {
		remaining, err := repo.RecordFailure(ctx, "alice", "192.0.2.10")
		if err != nil {
			t.Fatalf("RecordFailure #%d: %v", attempt, err)
		}
		if remaining != 0 {
			t.Fatalf("attempt #%d locked for %v before threshold", attempt, remaining)
		}
	}
	remaining, err := repo.RecordFailure(ctx, "alice", "192.0.2.10")
	if err != nil {
		t.Fatalf("RecordFailure threshold: %v", err)
	}
	if remaining <= 9*time.Minute || remaining > 10*time.Minute {
		t.Fatalf("remaining = %v, want within ten-minute window", remaining)
	}
	if checked, err := repo.LockRemaining(ctx, "alice", "198.51.100.20"); err != nil || checked <= 0 {
		t.Fatalf("username lock = %v/%v, want positive", checked, err)
	}

	mr.FastForward(10*time.Minute + time.Millisecond)
	if checked, err := repo.LockRemaining(ctx, "alice", "192.0.2.10"); err != nil || checked != 0 {
		t.Fatalf("expired lock = %v/%v, want 0/nil", checked, err)
	}
}

func TestSuccessfulLoginClearsOnlyUsernameCounter(t *testing.T) {
	repo, _ := newTestLoginCounterRepo(t)
	ctx := t.Context()
	for range 4 {
		if _, err := repo.RecordFailure(ctx, "alice", "192.0.2.10"); err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
	}
	if err := repo.ClearUserName(ctx, "alice"); err != nil {
		t.Fatalf("ClearUserName: %v", err)
	}
	// The same username starts from zero, while the source-IP count continues
	// and reaches its independent threshold after sixteen more failures.
	if remaining, err := repo.RecordFailure(ctx, "alice", "198.51.100.20"); err != nil || remaining != 0 {
		t.Fatalf("username was not cleared: %v/%v", remaining, err)
	}
	for attempt := 1; attempt <= 16; attempt++ {
		remaining, err := repo.RecordFailure(ctx, fmt.Sprintf("source-only-%d", attempt), "192.0.2.10")
		if err != nil {
			t.Fatalf("source failure #%d: %v", attempt, err)
		}
		if attempt < 16 && remaining != 0 {
			t.Fatalf("source locked early at #%d: %v", attempt, remaining)
		}
		if attempt == 16 && remaining <= 0 {
			t.Fatal("source-IP counter was incorrectly cleared with username")
		}
	}
}

func TestLoginCounterBackendFailureMapsToServiceUnavailable(t *testing.T) {
	repo, mr := newTestLoginCounterRepo(t)
	mr.Close()
	_, err := repo.LockRemaining(t.Context(), "alice", "192.0.2.10")
	if !errors.Is(err, biz.ErrAuthBackendUnavailable) {
		t.Fatalf("LockRemaining error = %v, want ErrAuthBackendUnavailable", err)
	}
}
