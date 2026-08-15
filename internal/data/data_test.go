package data

import (
	"testing"
	"time"

	"glex/internal/conf"

	"github.com/alicebob/miniredis/v2"
	"google.golang.org/protobuf/types/known/durationpb"
)

// TestNewRedisClientLifecycle covers the shared client's construction,
// reachability gate, and cleanup: alive while miniredis runs, closed (and
// thus unusable) after Close.
func TestNewRedisClientLifecycle(t *testing.T) {
	mr := miniredis.RunT(t)
	c := &conf.Data{
		Redis: &conf.Data_Redis{
			Addr:         mr.Addr(),
			ReadTimeout:  durationpb.New(time.Second),
			WriteTimeout: durationpb.New(time.Second),
		},
	}

	rdb, readTimeout, writeTimeout, err := newRedisClient(c)
	if err != nil {
		t.Fatalf("newRedisClient: %v", err)
	}
	if readTimeout != time.Second || writeTimeout != time.Second {
		t.Fatalf("timeouts = %v/%v, want 1s/1s", readTimeout, writeTimeout)
	}

	storage := newAuthStorage(rdb, "glex:auth", 0, 0)
	if err := storage.Ping(); err != nil {
		t.Fatalf("Ping before close: %v", err)
	}

	rdb.Close()
	if err := storage.Ping(); err == nil {
		t.Fatal("Ping after Close must fail")
	}
}

// TestNewRedisClientFailsWhenUnreachable proves the startup reachability
// gate: the app refuses to serve when Redis is down instead of failing per
// request later.
func TestNewRedisClientFailsWhenUnreachable(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close() // port now closed

	c := &conf.Data{
		Redis: &conf.Data_Redis{
			Addr:         addr,
			ReadTimeout:  durationpb.New(150 * time.Millisecond),
			WriteTimeout: durationpb.New(150 * time.Millisecond),
		},
	}
	if _, _, _, err := newRedisClient(c); err == nil {
		t.Fatal("newRedisClient against a closed Redis must fail")
	}
}

// TestNewRedisClientDefaultsTimeouts pins the fallback when the config omits
// timeouts.
func TestNewRedisClientDefaultsTimeouts(t *testing.T) {
	mr := miniredis.RunT(t)
	c := &conf.Data{Redis: &conf.Data_Redis{Addr: mr.Addr()}}
	rdb, readTimeout, writeTimeout, err := newRedisClient(c)
	if err != nil {
		t.Fatalf("newRedisClient: %v", err)
	}
	defer rdb.Close()
	if readTimeout != defaultAuthReadTimeout || writeTimeout != defaultAuthWriteTimeout {
		t.Fatalf("defaults = %v/%v, want %v/%v", readTimeout, writeTimeout, defaultAuthReadTimeout, defaultAuthWriteTimeout)
	}
}
