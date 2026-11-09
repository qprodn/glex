package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"glex/internal/conf"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/migrate"
	"glex/internal/infra/snowflake"
)

// The generated ent client opens the "postgres" dialect, and pgx's stdlib
// registers itself under "pgx". Register pgx's driver under the dialect name
// so ent.Open("postgres", …) reaches a real driver — without this, no
// PostgreSQL connection (application or test) can be opened at all. The
// drivers() guard keeps the registration from panicking if another module
// ever supplies a "postgres" driver first.
func init() {
	if !slices.Contains(sql.Drivers(), "postgres") {
		sql.Register("postgres", stdlib.GetDefaultDriver())
	}
}

const (
	DefaultAuthReadTimeout  = 200 * time.Millisecond
	DefaultAuthWriteTimeout = 200 * time.Millisecond
)

// Data is a struct that contains the database client. db is the generated
// transaction-aware Database wrapper: repos resolve entity clients from ctx
// (db.User(ctx), …) so every call inside a Transaction joins it.
type Data struct {
	DB     *ent.Database
	Logger *slog.Logger
	// rdb is the long-lived Redis client shared by the auth storage adapter
	// and the Login Session store.
	Redis *redis.Client
	// redisReadTimeout/redisWriteTimeout bound every auth storage operation;
	// kept here so repos build adapters without re-reading config.
	RedisReadTimeout  time.Duration
	RedisWriteTimeout time.Duration
}

type dataResourceCloser interface {
	Close() error
}

// NewData creates a new Data instance. The ent client keeps owning the
// connection pool (and the cleanup closes it); Data exposes it only through
// the transaction-aware Database wrapper. It also installs the process's
// snowflake node — every module's ID minting delegates to it, so the
// configured node number must be distinct per replica (ADR 0002's ID
// convention; the collision path is real under the job scheduler's
// symmetric replicas).
func NewData(c *conf.Data, logger *slog.Logger) (*Data, func(), error) {
	logger = logger.With(slog.String("component", "infra.store"))
	node := int64(c.GetSnowflakeNode())
	if node == 0 {
		node = 1
	}
	if err := snowflake.Init(node); err != nil {
		return nil, nil, fmt.Errorf("init snowflake node: %w", err)
	}
	client, err := ent.Open(c.Database.Driver, c.Database.Source)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	if os.Getenv("DEPLOY_ENV") == "dev" {
		// Enable debug mode for detailed logging.
		client = client.Debug()
		// Run the auto migration tool.
		if err = client.Schema.Create(context.Background(), migrate.WithDropIndex(true)); err != nil {
			return nil, nil, err
		}
	}
	rdb, readTimeout, writeTimeout, err := newRedisClient(c)
	if err != nil {
		return nil, nil, err
	}
	cleanup := newDataCleanup(logger, client, rdb)
	return &Data{
		DB:                ent.NewDatabaseFromClient(client),
		Logger:            logger,
		Redis:             rdb,
		RedisReadTimeout:  readTimeout,
		RedisWriteTimeout: writeTimeout,
	}, cleanup, nil
}

// newDataCleanup closes the database and Redis as independent resources. It is
// safe to call more than once, and a close error from either client never
// prevents the other client from being closed.
func newDataCleanup(logger *slog.Logger, db, rdb dataResourceCloser) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := db.Close(); err != nil {
				logger.Warn("failed closing database client", slog.String("operation", "store.close_database"), slog.Any("error", err))
			}
			if err := rdb.Close(); err != nil {
				logger.Warn("failed closing Redis client", slog.String("operation", "store.close_redis"), slog.Any("error", err))
			}
		})
	}
}

// newRedisClient builds the shared Redis client from config, bounds every
// operation via the client's own read/write timeouts, and proves the backend
// is reachable before the app starts serving.
func newRedisClient(c *conf.Data) (*redis.Client, time.Duration, time.Duration, error) {
	cfg := c.GetRedis()
	readTimeout := BoundedTimeout(cfg.GetReadTimeout().AsDuration(), DefaultAuthReadTimeout)
	writeTimeout := BoundedTimeout(cfg.GetWriteTimeout().AsDuration(), DefaultAuthWriteTimeout)
	options := &redis.Options{
		Network:      cfg.GetNetwork(),
		Addr:         cfg.GetAddr(),
		DialTimeout:  readTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		PoolTimeout:  readTimeout + writeTimeout,
	}
	rdb := redis.NewClient(options)
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout+writeTimeout)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, 0, 0, err
	}
	return rdb, readTimeout, writeTimeout, nil
}

// BoundedTimeout falls back to def when d is unset.
func BoundedTimeout(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}
