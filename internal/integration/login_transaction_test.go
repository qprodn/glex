package integration

import (
	"strings"
	"testing"
	"time"

	authnapp "glex/internal/application/authn"
	accessimpl "glex/internal/crossdomain/access/impl"
	authndomain "glex/internal/domain/authn"
	accessinfra "glex/internal/infra/access"
	authninfra "glex/internal/infra/authn"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/menu"
	"glex/internal/infra/ent/role"
	"glex/internal/infra/ent/user"
	"glex/internal/infra/store"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core"
)

// newLoginOrderingGraph builds the full login stack over sqlmock PostgreSQL
// and miniredis-backed sa-token/session stores, composed from the modules'
// exported constructors only.
func newLoginOrderingGraph(t *testing.T) (*store.Data, *core.Manager, authndomain.LoginSessionRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	conf := testAuthConf()
	data := &store.Data{DB: ent.NewDatabaseFromClient(client), Redis: rdb, Logger: testLogger(), RedisReadTimeout: time.Second, RedisWriteTimeout: time.Second}
	manager, cleanup, err := authninfra.NewAuthManager(authninfra.NewAuthStorage(data, conf), conf)
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanup)
	sessions := authninfra.NewLoginSessionRepo(data, manager, conf)
	return data, manager, sessions, mock
}

// TestLoginRunsDatabasePrimitivesInOrderAndKeepsRedisLast drives the full
// production login usecase over a sqlmock graph: the access crossdomain
// contract resolves the Authorization Snapshot on the same
// context-propagated transaction, so the database primitives stay inside one
// transaction and the Redis primitive runs last. The proof spans the
// cross-module seam, which is why it lives with the whole-graph suites.
func TestLoginRunsDatabasePrimitivesInOrderAndKeepsRedisLast(t *testing.T) {
	data, manager, sessions, mock := newLoginOrderingGraph(t)
	loginTime := time.Unix(1_700_000_000, 0)

	// Pre-transaction account lookup.
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), "alice", "Alice", nil, nil, int8(0), int8(0), integrationPasswordHash(), "", "", int64(0), int64(0), "", nil, loginTime, loginTime, nil,
		))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), "alice", "Alice", nil, nil, int8(0), int8(0), integrationPasswordHash(), "", "", int64(0), int64(0), "", nil, loginTime, loginTime, nil,
		))
	mock.ExpectQuery(`SELECT .* FROM "sys_role"`).
		WillReturnRows(sqlmock.NewRows(role.Columns).AddRow(
			int64(7), "Super administrator", "superadmin", 1, int8(0), true, "", int64(0), int64(0), loginTime, loginTime, nil,
		))
	mock.ExpectQuery(`SELECT .* FROM "sys_menu"`).
		WillReturnRows(sqlmock.NewRows(menu.Columns).AddRow(
			int64(9), "List users", int64(0), 1, "", "", "", int8(0), int8(1), int8(3), int8(0), int8(0), "system:user:list", "", "", "", int64(0), int64(0), loginTime, loginTime, nil,
		))
	mock.ExpectExec(`UPDATE "sys_user" SET .*"login_ip"\s*=.*"login_date"\s*=`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// ent's update-one path re-reads the node it touched.
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), "alice", "Alice", nil, nil, int8(0), int8(0), integrationPasswordHash(), "", "", int64(0), int64(0), "192.0.2.10", loginTime, loginTime, loginTime, nil,
		))
	mock.ExpectCommit()

	authorizer := accessimpl.NewAuthorizationResolver(accessinfra.NewAuthorizationResolver(data))
	uc := authnapp.NewLoginUsecase(
		authninfra.NewAuthLoginBackend(data, manager, sessions, testAuthConf()),
		authninfra.NewLoginCounterRepo(data, testAuthConf()),
		sessions, authorizer, store.NewTxManager(data), testLogger())
	result, err := uc.Login(t.Context(), &authnapp.LoginRequest{
		UserName:  "alice",
		Password:  integrationLoginPassword,
		SourceIP:  "192.0.2.10",
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) Firefox/1.0",
	})
	if err != nil {
		t.Fatalf("login orchestration: %v", err)
	}
	if result.AccessToken == "" || result.ExpiresIn != 7*24*time.Hour {
		t.Fatalf("LoginResult = %#v", result)
	}
	listed, err := sessions.ListBySystemUser(t.Context(), 42)
	if err != nil || len(listed) != 1 {
		t.Fatalf("authoritative Login Sessions = %v/%v", listed, err)
	}
	if listed[0].ID == result.AccessToken || listed[0].Browser != "Firefox" || listed[0].IPAddress != "192.0.2.10" {
		t.Fatalf("safe Session metadata = %#v", listed[0])
	}
	permissions, err := manager.GetPermissions("42")
	if err != nil || strings.Join(permissions, ",") != "*:*:*,system:user:list" {
		t.Fatalf("snapshot permissions = %v/%v", permissions, err)
	}
	roles, err := manager.GetRoles("42")
	if err != nil || strings.Join(roles, ",") != "superadmin" {
		t.Fatalf("snapshot roles = %v/%v", roles, err)
	}
	// sqlmock enforces statement order; every expectation was consumed before
	// the Redis step, so no database statement ran after it.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
