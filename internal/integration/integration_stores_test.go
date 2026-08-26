package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	accessapp "glex/internal/application/access"
	authnapp "glex/internal/application/authn"
	userapp "glex/internal/application/user"
	"glex/internal/conf"
	accesscrossdomain "glex/internal/crossdomain/access"
	accessimpl "glex/internal/crossdomain/access/impl"
	authncrossdomain "glex/internal/crossdomain/authn"
	authnimpl "glex/internal/crossdomain/authn/impl"
	access "glex/internal/domain/access"
	authndomain "glex/internal/domain/authn"
	userdomain "glex/internal/domain/user"
	accessinfra "glex/internal/infra/access"
	authninfra "glex/internal/infra/authn"
	"glex/internal/infra/ent"
	"glex/internal/infra/store"
	userinfra "glex/internal/infra/user"

	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// The whole-graph integration suites construct the user, access, and authn
// modules together through the same constructors the Wire composition root
// binds, so they are the safety net for the cross-module seams of ADR 0016:
// login fills its Authorization Snapshot through the access crossdomain
// contract, and cross-module security changes revoke through the authn
// contract. They run against real PostgreSQL and Redis, gated on two
// environment variables the runner supplies:
//
//	GLEX_TEST_POSTGRES_DSN  e.g. postgres://postgres@127.0.0.1:5432/glex_test?sslmode=disable
//	GLEX_TEST_REDIS_ADDR    e.g. 127.0.0.1:6379
//
// No credential is embedded here. Each test owns a unique glex auth namespace
// and unlinks it on cleanup, so repeated runs against the same stores never
// observe each other's state.
const (
	integrationPostgresDSNEnv = "GLEX_TEST_POSTGRES_DSN"
	integrationRedisAddrEnv   = "GLEX_TEST_REDIS_ADDR"
)

func testAuthConf() *conf.Auth {
	return &conf.Auth{
		FixedLifetime:                  durationpb.New(7 * 24 * time.Hour),
		IdleLifetime:                   durationpb.New(30 * time.Minute),
		MaxSessionsPerUser:             5,
		RedisNamespace:                 "glex:auth",
		EdgeProxyAuthoritativeSourceIp: true,
		UsernameLoginCounter: &conf.Auth_LoginCounter{
			MaxFailures: 5,
			Window:      durationpb.New(10 * time.Minute),
		},
		SourceIpLoginCounter: &conf.Auth_LoginCounter{
			MaxFailures: 20,
			Window:      durationpb.New(10 * time.Minute),
		},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// normalizeNamespace mirrors the authn storage adapter's namespace rule so
// tests can address the raw Redis keys a graph writes.
func normalizeNamespace(ns string) string {
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return ""
	}
	return strings.TrimSuffix(ns, ":") + ":"
}

// integrationStores describes the shared real stores for one test: a DSN, a
// Redis address, and the auth configuration (with a per-test namespace) every
// provider graph must agree on. Opening clients is left to the graphs so a
// test can run several independent ones.
type integrationStores struct {
	conf      *conf.Auth
	dsn       string
	redisAddr string
	logger    *slog.Logger

	schemaOnce sync.Once
	schemaErr  error
}

func newIntegrationStores(t *testing.T, configure ...func(*conf.Auth)) *integrationStores {
	t.Helper()
	dsn := os.Getenv(integrationPostgresDSNEnv)
	redisAddr := os.Getenv(integrationRedisAddrEnv)
	if dsn == "" || redisAddr == "" {
		t.Skipf("set %s and %s for real PostgreSQL/Redis integration tests",
			integrationPostgresDSNEnv, integrationRedisAddrEnv)
	}
	conf := proto.Clone(testAuthConf()).(*conf.Auth)
	conf.RedisNamespace = fmt.Sprintf("glex:test:%s:%d", t.Name(), time.Now().UnixNano())
	for _, mutate := range configure {
		mutate(conf)
	}
	return &integrationStores{conf: conf, dsn: dsn, redisAddr: redisAddr, logger: testLogger()}
}

// createSchema migrates the shared database at most once per test, before any
// graph runs queries, so two graphs never race the migration.
func (s *integrationStores) createSchema(t *testing.T) {
	t.Helper()
	s.schemaOnce.Do(func() {
		client, err := ent.Open("postgres", s.dsn)
		if err != nil {
			s.schemaErr = err
			return
		}
		defer client.Close()
		s.schemaErr = client.Schema.Create(context.Background())
	})
	if s.schemaErr != nil {
		t.Fatalf("create integration schema: %v", s.schemaErr)
	}
}

// cleanupNamespace unlinks every key this test wrote in its glex auth
// namespace. It uses a fresh client so it still works after a graph's client
// was closed by a failure-injection test.
func (s *integrationStores) cleanupNamespace() {
	rdb := redis.NewClient(&redis.Options{Addr: s.redisAddr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	keys, err := rdb.Keys(ctx, s.namespacePattern()).Result()
	if err == nil && len(keys) > 0 {
		_ = rdb.Unlink(ctx, keys...).Err()
	}
}

// rawKey maps a glex auth key onto the full Redis key every graph uses, so
// tests can inspect and corrupt storage directly.
func (s *integrationStores) rawKey(key string) string {
	return normalizeNamespace(s.conf.GetRedisNamespace()) + key
}

func (s *integrationStores) namespacePattern() string {
	return normalizeNamespace(s.conf.GetRedisNamespace()) + "*"
}

// adminRedis opens a maintenance client independent of any graph — usable
// after failure-injection tests closed the graphs' own clients.
func (s *integrationStores) adminRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: s.redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// authGraph is one independent application/provider graph — its own PostgreSQL
// and Redis clients and its own Manager — composing the three modules through
// their exported constructors the way cmd wires them in production (Data and
// the storage adapter are built directly so every graph can point at the
// shared stores). Two graphs over the same stores behave like two service
// instances: all shared state must live in PostgreSQL and Redis, never in
// process memory.
type authGraph struct {
	data         *store.Data
	db           *ent.Client
	rdb          *redis.Client
	manager      *core.Manager
	sessions     authndomain.LoginSessionRepo
	login        authndomain.AuthLoginBackend
	authorizer   authndomain.Authenticator
	tx           *store.TxManager
	revoker      authncrossdomain.SessionRevoker
	counter      authndomain.LoginCounterRepo
	resolver     accesscrossdomain.AuthorizationResolver
	users        userdomain.UserRepo
	roles        access.RoleRepo
	menus        access.MenuRepo
	userRoles    access.UserRoleRepo
	userUC       *userapp.UserUsecase
	roleUC       *accessapp.RoleUsecase
	menuUC       *accessapp.MenuUsecase
	authUC       *authnapp.LoginUsecase
	assignmentUC *accessapp.RoleAssignmentUsecase
}

func (s *integrationStores) newGraph(t *testing.T) *authGraph {
	t.Helper()
	s.createSchema(t)
	db, err := ent.Open("postgres", s.dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL graph client: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rdb := redis.NewClient(&redis.Options{Addr: s.redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	data := &store.Data{
		DB:                ent.NewDatabaseFromClient(db),
		Redis:             rdb,
		Logger:            s.logger,
		RedisReadTimeout:  time.Second,
		RedisWriteTimeout: time.Second,
	}
	manager, cleanupManager, err := authninfra.NewAuthManager(authninfra.NewAuthStorage(data, s.conf), s.conf)
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanupManager)
	sessions := authninfra.NewLoginSessionRepo(data, manager, s.conf)
	revoker := authnimpl.NewSessionRevoker(sessions)
	tx := store.NewTxManager(data)
	counter := authninfra.NewLoginCounterRepo(data, s.conf)
	login := authninfra.NewAuthLoginBackend(data, manager, sessions, s.conf)
	users := userinfra.NewUserRepo(data)
	roles := accessinfra.NewRoleRepo(data)
	menus := accessinfra.NewMenuRepo(data)
	userRoles := accessinfra.NewUserRoleRepo(data)
	resolver := accessimpl.NewAuthorizationResolver(accessinfra.NewAuthorizationResolver(data))
	t.Cleanup(s.cleanupNamespace)
	return &authGraph{
		data:         data,
		db:           db,
		rdb:          rdb,
		manager:      manager,
		sessions:     sessions,
		login:        login,
		authorizer:   authninfra.NewAuthenticator(manager, sessions),
		tx:           tx,
		revoker:      revoker,
		counter:      counter,
		resolver:     resolver,
		users:        users,
		roles:        roles,
		menus:        menus,
		userRoles:    userRoles,
		userUC:       userapp.NewUserUsecase(users, authnimpl.NewCredentialWriter(authninfra.NewCredentialRepo(data)), tx, revoker, s.logger),
		roleUC:       accessapp.NewRoleUsecase(roles, menus, userRoles, tx, revoker, s.logger),
		menuUC:       accessapp.NewMenuUsecase(menus, tx, revoker, s.logger),
		authUC:       authnapp.NewLoginUsecase(login, counter, sessions, resolver, tx, s.logger),
		assignmentUC: accessapp.NewRoleAssignmentUsecase(roles, userRoles, revoker, tx, s.logger),
	}
}

// integrationSeeder hands out collision-free Snowflake-range IDs and seeds the
// shared PostgreSQL through one graph's client.
type integrationSeeder struct {
	nextID atomic.Int64
}

func newIntegrationSeeder() *integrationSeeder {
	s := &integrationSeeder{}
	s.nextID.Store(time.Now().UnixNano() & (1<<62 - 1))
	return s
}

func (s *integrationSeeder) id() int64 { return s.nextID.Add(1) }

// integrationLoginPassword is the plaintext every seeded user authenticates
// with; the stored hash is real bcrypt so the domain login usecase verifies it
// end-to-end.
const integrationLoginPassword = "integration-password"

var integrationPasswordHash = sync.OnceValue(func() string {
	hashed, err := bcrypt.GenerateFromPassword([]byte(integrationLoginPassword), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return string(hashed)
})

func (s *integrationSeeder) user(t *testing.T, g *authGraph, status int8) int64 {
	t.Helper()
	id := s.id()
	_, err := g.db.User.Create().
		SetID(id).
		SetUserName(integrationUserName(id)).
		SetNickName("User").
		SetStatus(status).
		SetPassword(integrationPasswordHash()).
		Save(t.Context())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func integrationUserName(id int64) string { return "user-" + strconv.FormatInt(id, 10) }

func (s *integrationSeeder) role(t *testing.T, g *authGraph, status int8) int64 {
	t.Helper()
	id := s.id()
	_, err := g.db.Role.Create().
		SetID(id).
		SetRoleName("role-" + strconv.FormatInt(id, 10)).
		SetRoleKey("key-" + strconv.FormatInt(id, 10)).
		SetStatus(status).
		Save(t.Context())
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	return id
}

func (s *integrationSeeder) menu(t *testing.T, g *authGraph, perms string, status int8) int64 {
	t.Helper()
	id := s.id()
	_, err := g.db.Menu.Create().
		SetID(id).
		SetMenuName("menu-" + strconv.FormatInt(id, 10)).
		SetMenuType(int8(access.MenuTypeButton)).
		SetPerms(perms).
		SetStatus(status).
		Save(t.Context())
	if err != nil {
		t.Fatalf("create menu: %v", err)
	}
	return id
}

// loginOnGraph runs the full production login usecase — counter gate, bcrypt
// verify, revalidation, Authorization Resolution through the access
// crossdomain contract, last-login write, credential issuance — on one graph,
// and returns the registered Session plus the bearer credential exactly once.
func loginOnGraph(t *testing.T, g *authGraph, systemUserID int64, deviceType string) (*authndomain.LoginSession, string) {
	t.Helper()
	session, token, err := issueOnGraph(t.Context(), g, systemUserID, deviceType)
	if err != nil {
		t.Fatalf("login on graph: %v", err)
	}
	return session, token
}

// issueOnGraph is the goroutine-safe core of loginOnGraph: it reports errors
// instead of failing the test, so caller goroutines can propagate them to the
// test goroutine (t.Fatalf must never run off the test goroutine).
func issueOnGraph(ctx context.Context, g *authGraph, systemUserID int64, deviceType string) (*authndomain.LoginSession, string, error) {
	result, err := g.authUC.Login(ctx, &authnapp.LoginRequest{
		UserName:  integrationUserName(systemUserID),
		Password:  integrationLoginPassword,
		SourceIP:  "192.0.2.1",
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) Firefox/1.0",
	})
	if err != nil {
		return nil, "", err
	}
	session, err := g.sessions.FindByToken(ctx, result.AccessToken)
	if err != nil {
		return nil, "", err
	}
	return session, result.AccessToken, nil
}

// requireRevoked asserts a Session ID is gone from the authoritative index of
// every graph given.
func requireRevoked(t *testing.T, sessionID string, graphs ...*authGraph) {
	t.Helper()
	for _, g := range graphs {
		if _, err := g.sessions.FindByID(t.Context(), sessionID); !errors.Is(err, authndomain.ErrLoginSessionNotFound) {
			t.Fatalf("Session %q remains usable/indexed: %v", sessionID, err)
		}
	}
}
