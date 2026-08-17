package data

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/wire"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"glex/internal/conf"
	"glex/internal/data/ent"
	"glex/internal/data/ent/migrate"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,
	NewUserRepo,
	NewMenuRepo,
	NewRoleRepo,
	NewUserRoleRepo,
	NewAuthStorage,
	NewAuthManager,
	NewLoginSessionRepo,
	NewSecuritySessionRevoker,
	NewSecurityChangeCoordinator,
	NewAuthenticator,
	NewLoginCounterRepo,
	NewAuthLoginRepo,
)

// Data is a struct that contains the database client.
type Data struct {
	db *ent.Client
	// rdb is the long-lived Redis client shared by the auth storage adapter
	// and the Login Session store.
	rdb *redis.Client
	// redisReadTimeout/redisWriteTimeout bound every auth storage operation;
	// kept here so repos build adapters without re-reading config.
	redisReadTimeout  time.Duration
	redisWriteTimeout time.Duration
}

type dataResourceCloser interface {
	Close() error
}

// NewData creates a new Data instance.
func NewData(c *conf.Data) (*Data, func(), error) {
	db, err := ent.Open(c.Database.Driver, c.Database.Source)
	if err != nil {
		log.Fatalf("failed opening connection to database: %v", err)
	}
	if os.Getenv("DEPLOY_ENV") == "dev" {
		// Enable debug mode for detailed logging.
		db = db.Debug()
		// Run the auto migration tool.
		if err = db.Schema.Create(context.Background(), migrate.WithDropIndex(true)); err != nil {
			return nil, nil, err
		}
	}
	rdb, readTimeout, writeTimeout, err := newRedisClient(c)
	if err != nil {
		return nil, nil, err
	}
	cleanup := newDataCleanup(db, rdb)
	return &Data{
		db:                db,
		rdb:               rdb,
		redisReadTimeout:  readTimeout,
		redisWriteTimeout: writeTimeout,
	}, cleanup, nil
}

// newDataCleanup closes the database and Redis as independent resources. It is
// safe to call more than once, and a close error from either client never
// prevents the other client from being closed.
func newDataCleanup(db, rdb dataResourceCloser) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := db.Close(); err != nil {
				log.Printf("failed closing database client: %v", err)
			}
			if err := rdb.Close(); err != nil {
				log.Printf("failed closing Redis client: %v", err)
			}
		})
	}
}

// newRedisClient builds the shared Redis client from config, bounds every
// operation via the client's own read/write timeouts, and proves the backend
// is reachable before the app starts serving.
func newRedisClient(c *conf.Data) (*redis.Client, time.Duration, time.Duration, error) {
	cfg := c.GetRedis()
	readTimeout := boundedTimeout(cfg.GetReadTimeout().AsDuration(), defaultAuthReadTimeout)
	writeTimeout := boundedTimeout(cfg.GetWriteTimeout().AsDuration(), defaultAuthWriteTimeout)
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
