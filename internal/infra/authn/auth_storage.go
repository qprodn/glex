package authninfra

import (
	"glex/internal/infra/store"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"glex/internal/conf"

	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core/adapter"
)

// ErrAuthKeyNotFound is returned by the auth storage adapter when a key is
// absent — a normal state, distinct from a backend failure.
var ErrAuthKeyNotFound = errors.New("auth storage: key not found")

// ErrAuthStorage marks Redis backend failures (unreachable, timed out,
// protocol errors). Callers must map it to 503, never to 401/403.
var ErrAuthStorage = errors.New("auth storage: backend failure")

const (
	authScanCount   = 100
	authUnlinkBatch = 500
)

// authStorage implements sa-token's adapter.Storage over the shared Redis
// client. Every key lives under the configured glex auth namespace, each
// operation carries a bounded timeout, and missing keys are reported as
// ErrAuthKeyNotFound so backend failures stay distinguishable.
type authStorage struct {
	rdb          *redis.Client
	logger       *slog.Logger
	namespace    string
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// NewAuthStorage builds the sa-token storage adapter from the shared Redis
// client owned by Data. It returns the adapter.Storage interface so the
// concrete type never leaves data.
func NewAuthStorage(d *store.Data, c *conf.Auth) adapter.Storage {
	return newAuthStorage(
		d.Redis,
		c.GetRedisNamespace(),
		d.RedisReadTimeout,
		d.RedisWriteTimeout,
		d.Logger.With(slog.String("component", "infra.authn.auth_storage")),
	)
}

// newAuthStorage is the internal constructor tests use with their own client.
func newAuthStorage(rdb *redis.Client, namespace string, readTimeout, writeTimeout time.Duration, logger *slog.Logger) adapter.Storage {
	return &authStorage{
		rdb:          rdb,
		logger:       logger,
		namespace:    normalizeNamespace(namespace),
		readTimeout:  store.BoundedTimeout(readTimeout, store.DefaultAuthReadTimeout),
		writeTimeout: store.BoundedTimeout(writeTimeout, store.DefaultAuthWriteTimeout),
	}
}

// normalizeNamespace returns ns with a single trailing colon separator, or ""
// when no namespace is configured.
func normalizeNamespace(ns string) string {
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return ""
	}
	return strings.TrimSuffix(ns, ":") + ":"
}

// key maps a sa-token key into the glex auth namespace.
func (s *authStorage) key(key string) string {
	return s.namespace + key
}

// unkey strips the namespace from a raw Redis key; keys outside the namespace
// are returned unchanged.
func (s *authStorage) unkey(raw string) string {
	return strings.TrimPrefix(raw, s.namespace)
}

func (s *authStorage) readCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.readTimeout)
}

func (s *authStorage) writeCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.writeTimeout)
}

// keyClass returns the non-secret head of a sa-token key ("token:",
// "account:") for logs and errors. The remainder of keys like
// token:<bearer> is a credential and must never be written anywhere.
func keyClass(key string) string {
	if i := strings.IndexByte(key, ':'); i >= 0 {
		return key[:i+1]
	}
	return key
}

// observe is the logging hook every operation funnels through: backend
// failures surface here at Error with the operation and key class (ADR 0007).
// Missing keys and canceled requests are normal states and stay silent.
// Metrics (when the repo adopts a metrics stack) attach at this same seam.
func (s *authStorage) observe(op, key string, start time.Time, err error) {
	if err != nil && !errors.Is(err, redis.Nil) && !errors.Is(err, ErrAuthKeyNotFound) && !errors.Is(err, context.Canceled) {
		s.logger.Error("auth storage operation failed",
			slog.String("operation", op),
			slog.String("key_class", keyClass(key)),
			slog.Any("error", err),
			slog.Duration("duration", time.Since(start)),
		)
	}
}

// marshalValue converts a sa-token value into what Redis stores. sa-token
// only ever writes strings; anything else is JSON-encoded so the value stays
// round-trippable instead of failing the write.
func marshalValue(value any) any {
	switch v := value.(type) {
	case string, []byte:
		return v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return encoded
	}
}

func (s *authStorage) Set(key string, value any, expiration time.Duration) error {
	ctx, cancel := s.writeCtx()
	defer cancel()
	start := time.Now()
	err := s.rdb.Set(ctx, s.key(key), marshalValue(value), expiration).Err()
	s.observe("authn.storage_set", key, start, err)
	return s.classify(err)
}

func (s *authStorage) SetKeepTTL(key string, value any) error {
	ctx, cancel := s.writeCtx()
	defer cancel()
	start := time.Now()
	// SET ... KEEPTTL XX in one round trip: without XX, a key expiring
	// between a separate exists-check and the write would be resurrected
	// without a TTL.
	err := s.rdb.SetArgs(ctx, s.key(key), marshalValue(value), redis.SetArgs{
		KeepTTL: true,
		Mode:    "XX",
	}).Err()
	s.observe("authn.storage_setkeepttl", key, start, err)
	if errors.Is(err, redis.Nil) {
		return fmt.Errorf("%w: %s*", ErrAuthKeyNotFound, keyClass(key))
	}
	return s.classify(err)
}

func (s *authStorage) Get(key string) (any, error) {
	ctx, cancel := s.readCtx()
	defer cancel()
	start := time.Now()
	value, err := s.rdb.Get(ctx, s.key(key)).Result()
	s.observe("authn.storage_get", key, start, err)
	if errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("%w: %s*", ErrAuthKeyNotFound, keyClass(key))
	}
	if err != nil {
		return nil, s.classify(err)
	}
	return value, nil
}

func (s *authStorage) Delete(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	ctx, cancel := s.writeCtx()
	defer cancel()
	full := make([]string, len(keys))
	for i, key := range keys {
		full[i] = s.key(key)
	}
	start := time.Now()
	err := s.rdb.Del(ctx, full...).Err()
	s.observe("authn.storage_delete", keys[0], start, err)
	return s.classify(err)
}

// Exists reports whether a key is present. The upstream adapter.Storage
// signature forces a bool, so backend failures can only be logged here —
// auth decisions must use Get/Delete and branch on the typed errors, never
// on this bool, or an outage would read as "not logged in".
func (s *authStorage) Exists(key string) bool {
	ctx, cancel := s.readCtx()
	defer cancel()
	start := time.Now()
	result, err := s.rdb.Exists(ctx, s.key(key)).Result()
	s.observe("authn.storage_exists", key, start, err)
	return err == nil && result > 0
}

func (s *authStorage) Keys(pattern string) ([]string, error) {
	ctx, cancel := s.readCtx()
	defer cancel()
	start := time.Now()
	var matched []string
	cursor := uint64(0)
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, s.key(pattern), authScanCount).Result()
		if err != nil {
			s.observe("authn.storage_keys", pattern, start, err)
			return nil, s.classify(err)
		}
		for _, raw := range keys {
			matched = append(matched, s.unkey(raw))
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return matched, nil
}

func (s *authStorage) Expire(key string, expiration time.Duration) error {
	ctx, cancel := s.writeCtx()
	defer cancel()
	start := time.Now()
	err := s.rdb.Expire(ctx, s.key(key), expiration).Err()
	s.observe("authn.storage_expire", key, start, err)
	return s.classify(err)
}

func (s *authStorage) TTL(key string) (time.Duration, error) {
	ctx, cancel := s.readCtx()
	defer cancel()
	start := time.Now()
	ttl, err := s.rdb.TTL(ctx, s.key(key)).Result()
	s.observe("authn.storage_ttl", key, start, err)
	if err != nil {
		return 0, s.classify(err)
	}
	return ttl, nil
}

// Clear removes only this namespace's keys. It can never clear the whole
// Redis database: the scan pattern is anchored to the namespace, and there is
// no FLUSHALL/FLUSHDB path anywhere in the adapter. Each scan page and unlink
// batch gets its own bounded context so a large namespace can't outgrow one
// timeout or hang unbounded.
func (s *authStorage) Clear() error {
	cursor := uint64(0)
	for {
		scanCtx, scanCancel := s.readCtx()
		keys, next, err := s.rdb.Scan(scanCtx, cursor, s.namespace+"*", authScanCount).Result()
		scanCancel()
		start := time.Now()
		if err != nil {
			s.observe("authn.storage_clear", s.namespace, start, err)
			return s.classify(err)
		}
		for len(keys) > 0 {
			batch := keys
			if len(batch) > authUnlinkBatch {
				batch = batch[:authUnlinkBatch]
			}
			unlinkCtx, unlinkCancel := s.writeCtx()
			err := s.rdb.Unlink(unlinkCtx, batch...).Err()
			unlinkCancel()
			if err != nil {
				s.observe("authn.storage_clear", s.namespace, start, err)
				return s.classify(err)
			}
			keys = keys[len(batch):]
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (s *authStorage) Ping() error {
	ctx, cancel := s.readCtx()
	defer cancel()
	start := time.Now()
	err := s.rdb.Ping(ctx).Err()
	s.observe("authn.storage_ping", "", start, err)
	return s.classify(err)
}

// classify maps go-redis errors onto the adapter's two failure kinds: a nil
// error stays nil, anything else is a backend failure.
func (s *authStorage) classify(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrAuthStorage, err)
}
