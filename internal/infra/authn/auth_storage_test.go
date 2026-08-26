package authninfra

import (
	"bytes"
	"errors"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core/adapter"
)

// newTestStorage wires an adapter over miniredis and also returns the raw
// client for asserting how keys landed in Redis.
func newTestStorage(t *testing.T, namespace string) (adapter.Storage, *redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return newAuthStorage(rdb, namespace, time.Second, time.Second, testLogger()), rdb, mr
}

func TestAuthStorageSetGetDelete(t *testing.T) {
	storage, _, _ := newTestStorage(t, "glex:auth")

	if err := storage.Set("token:abc", "v1", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := storage.Get("token:abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "v1" {
		t.Fatalf("Get = %v, want v1", got)
	}

	if err := storage.Delete("token:abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := storage.Get("token:abc"); !errors.Is(err, ErrAuthKeyNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrAuthKeyNotFound", err)
	}

	// Delete of nothing and of missing keys succeeds.
	if err := storage.Delete(); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if err := storage.Delete("missing"); err != nil {
		t.Fatalf("Delete(missing): %v", err)
	}
}

func TestAuthStorageGetMissingIsNotBackendFailure(t *testing.T) {
	storage, _, _ := newTestStorage(t, "glex:auth")

	_, err := storage.Get("no-such-key")
	if !errors.Is(err, ErrAuthKeyNotFound) {
		t.Fatalf("Get missing = %v, want ErrAuthKeyNotFound", err)
	}
	if errors.Is(err, ErrAuthStorage) {
		t.Fatalf("missing key must not classify as backend failure: %v", err)
	}
	if storage.Exists("no-such-key") {
		t.Fatal("Exists(missing) must be false")
	}
}

func TestAuthStorageTTLAndExpire(t *testing.T) {
	storage, _, _ := newTestStorage(t, "glex:auth")

	if err := storage.Set("k", "v", 90*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ttl, err := storage.TTL("k")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 90*time.Second {
		t.Fatalf("TTL = %v, want within (0, 90s]", ttl)
	}

	if err := storage.Expire("k", 45*time.Second); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	ttl, err = storage.TTL("k")
	if err != nil {
		t.Fatalf("TTL after Expire: %v", err)
	}
	if ttl <= 0 || ttl > 45*time.Second {
		t.Fatalf("TTL after Expire = %v, want within (0, 45s]", ttl)
	}

	// A key stored without expiration reports -1 (never expires).
	if err := storage.Set("forever", "v", 0); err != nil {
		t.Fatalf("Set(no ttl): %v", err)
	}
	ttl, err = storage.TTL("forever")
	if err != nil {
		t.Fatalf("TTL(forever): %v", err)
	}
	if ttl != -1 {
		t.Fatalf("TTL(forever) = %v, want -1", ttl)
	}
}

func TestAuthStorageSetKeepTTL(t *testing.T) {
	storage, _, _ := newTestStorage(t, "glex:auth")

	if err := storage.Set("k", "old", 120*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := storage.SetKeepTTL("k", "new"); err != nil {
		t.Fatalf("SetKeepTTL: %v", err)
	}
	got, err := storage.Get("k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "new" {
		t.Fatalf("Get = %v, want new", got)
	}
	ttl, err := storage.TTL("k")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 120*time.Second {
		t.Fatalf("TTL = %v, want the original TTL kept within (0, 120s]", ttl)
	}

	// KeepTTL on a missing key must fail instead of resurrecting the key
	// with no expiration.
	err = storage.SetKeepTTL("missing", "v")
	if !errors.Is(err, ErrAuthKeyNotFound) {
		t.Fatalf("SetKeepTTL(missing) = %v, want ErrAuthKeyNotFound", err)
	}
	if _, err := storage.Get("missing"); !errors.Is(err, ErrAuthKeyNotFound) {
		t.Fatalf("SetKeepTTL must not create the key: %v", err)
	}
}

func TestAuthStorageNamespaceIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	auth := newAuthStorage(rdb, "glex:auth", time.Second, time.Second, testLogger())
	other := newAuthStorage(rdb, "glex:other", time.Second, time.Second, testLogger())

	if err := auth.Set("token:x", "from-auth", 0); err != nil {
		t.Fatalf("auth.Set: %v", err)
	}
	if err := other.Set("token:x", "from-other", 0); err != nil {
		t.Fatalf("other.Set: %v", err)
	}

	if got, _ := auth.Get("token:x"); got != "from-auth" {
		t.Fatalf("auth sees %v, want from-auth", got)
	}
	if got, _ := other.Get("token:x"); got != "from-other" {
		t.Fatalf("other sees %v, want from-other", got)
	}

	// The values land under distinct namespaced raw keys.
	for _, raw := range []string{"glex:auth:token:x", "glex:other:token:x"} {
		if _, err := mr.Get(raw); err != nil {
			t.Fatalf("raw key %s missing: %v", raw, err)
		}
	}

	// A trailing colon in the configured namespace normalizes to the same
	// keyspace instead of doubling the separator.
	dotted := newAuthStorage(rdb, "glex:auth:", time.Second, time.Second, testLogger())
	if got, _ := dotted.Get("token:x"); got != "from-auth" {
		t.Fatalf("namespace with trailing colon sees %v, want from-auth", got)
	}

	// Deleting through one namespace leaves the other untouched.
	if err := auth.Delete("token:x"); err != nil {
		t.Fatalf("auth.Delete: %v", err)
	}
	if _, err := other.Get("token:x"); err != nil {
		t.Fatalf("other.Get after auth.Delete: %v", err)
	}
}

func TestAuthStorageKeysReturnsUnnamespacedKeys(t *testing.T) {
	storage, rdb, _ := newTestStorage(t, "glex:auth")

	for _, key := range []string{"token:1", "token:2", "account:1"} {
		if err := rdb.Set(t.Context(), "glex:auth:"+key, "v", 0).Err(); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	// A raw key outside the namespace must never leak into Keys results.
	if err := rdb.Set(t.Context(), "glex:other:token:9", "v", 0).Err(); err != nil {
		t.Fatalf("seed foreign: %v", err)
	}

	keys, err := storage.Keys("token:*")
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	sort.Strings(keys)
	if len(keys) != 2 || keys[0] != "token:1" || keys[1] != "token:2" {
		t.Fatalf("Keys = %v, want [token:1 token:2]", keys)
	}
}

func TestAuthStorageClearRemovesOnlyOwnNamespace(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	auth := newAuthStorage(rdb, "glex:auth", time.Second, time.Second, testLogger())
	other := newAuthStorage(rdb, "glex:other", time.Second, time.Second, testLogger())

	for _, key := range []string{"token:1", "account:1", "renew:2"} {
		if err := auth.Set(key, "v", 0); err != nil {
			t.Fatalf("auth.Set %s: %v", key, err)
		}
	}
	if err := other.Set("token:1", "v", 0); err != nil {
		t.Fatalf("other.Set: %v", err)
	}
	if err := rdb.Set(t.Context(), "totally-foreign", "v", 0).Err(); err != nil {
		t.Fatalf("seed foreign: %v", err)
	}

	if err := auth.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	for _, key := range []string{"token:1", "account:1", "renew:2"} {
		if _, err := auth.Get(key); !errors.Is(err, ErrAuthKeyNotFound) {
			t.Fatalf("Clear left %s behind: %v", key, err)
		}
	}
	if _, err := mr.Get("glex:other:token:1"); err != nil {
		t.Fatalf("Clear wiped another namespace: %v", err)
	}
	if _, err := mr.Get("totally-foreign"); err != nil {
		t.Fatalf("Clear wiped a foreign key: %v", err)
	}
}

func TestAuthStoragePing(t *testing.T) {
	storage, _, _ := newTestStorage(t, "glex:auth")
	if err := storage.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// TestAuthStorageBackendTimeout proves a Redis that accepts connections but
// never answers surfaces as ErrAuthStorage within the bounded operation
// timeout — never as a missing key.
func TestAuthStorageBackendTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	held := make(chan net.Conn, 8)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			held <- conn // hold open, never reply
		}
	}()
	t.Cleanup(func() {
		close(held)
	})

	timeout := 80 * time.Millisecond
	rdb := redis.NewClient(&redis.Options{
		Addr:         listener.Addr().String(),
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		PoolTimeout:  2 * timeout,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	storage := newAuthStorage(rdb, "glex:auth", timeout, timeout, testLogger())

	start := time.Now()
	_, err = storage.Get("token:x")
	elapsed := time.Since(start)
	if !errors.Is(err, ErrAuthStorage) {
		t.Fatalf("Get against dead backend = %v, want ErrAuthStorage", err)
	}
	if errors.Is(err, ErrAuthKeyNotFound) {
		t.Fatalf("backend timeout must not classify as missing key: %v", err)
	}
	if err := storage.Set("token:x", "v", 0); !errors.Is(err, ErrAuthStorage) {
		t.Fatalf("Set against dead backend = %v, want ErrAuthStorage", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("operations must stay bounded, took %v", elapsed)
	}
}

// TestAuthStorageNeverLogsBearerToken captures slog output while a backend
// fails on token-shaped keys: the bearer token must never appear in logs.
func TestAuthStorageNeverLogsBearerToken(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close() // connection refused → backend failure, enough for the log path
		}
	}()

	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	rdb := redis.NewClient(&redis.Options{Addr: listener.Addr().String()})
	t.Cleanup(func() { _ = rdb.Close() })
	storage := newAuthStorage(rdb, "glex:auth", 50*time.Millisecond, 50*time.Millisecond, logger)

	const bearer = "s3cret-bearer-value"
	_ = storage.Set("token:"+bearer, "v", 0)
	_, _ = storage.Get("token:" + bearer)

	if strings.Contains(buf.String(), bearer) {
		t.Fatalf("bearer token leaked into logs: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "auth storage operation failed") {
		t.Fatalf("backend failure emitted no warning log: %q", buf.String())
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestNormalizeNamespace pins how configured namespaces map onto raw key
// prefixes.
func TestNormalizeNamespace(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"  ":          "",
		"glex:auth":   "glex:auth:",
		"glex:auth:":  "glex:auth:",
		" glex:auth ": "glex:auth:",
	}
	for in, want := range cases {
		if got := normalizeNamespace(in); got != want {
			t.Errorf("normalizeNamespace(%q) = %q, want %q", in, got, want)
		}
	}
}
