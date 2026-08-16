package data

import (
	"errors"
	"strings"
	"testing"
	"time"

	"glex/internal/biz"
	"glex/internal/data/ent"
	"glex/internal/data/ent/menu"
	"glex/internal/data/ent/role"
	"glex/internal/data/ent/user"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core"
)

func issueTestBearer(t *testing.T, manager *core.Manager, systemUserID int64, userName, deviceType string) string {
	t.Helper()
	token, err := loginAndReplaceAuthorizationSnapshot(manager, &biz.AuthorizationSnapshot{
		SystemUserID: systemUserID,
		UserName:     userName,
	}, deviceType, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("issue test bearer: %v", err)
	}
	return token
}

func TestSecondLoginReplacesSharedAuthorizationSnapshotBeforeTokenReturns(t *testing.T) {
	_, manager, _ := newTestLoginSessionRepo(t)
	first, err := loginAndReplaceAuthorizationSnapshot(manager, &biz.AuthorizationSnapshot{
		SystemUserID: 42,
		UserName:     "alice",
		RoleKeys:     []string{"viewer"},
		Permissions:  []string{"system:user:list"},
	}, "desktop", 7*24*time.Hour)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	second, err := loginAndReplaceAuthorizationSnapshot(manager, &biz.AuthorizationSnapshot{
		SystemUserID: 42,
		UserName:     "alice",
		RoleKeys:     []string{"operator"},
		Permissions:  []string{"system:user:edit"},
	}, "mobile", 7*24*time.Hour)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if first == second || first == "" || second == "" {
		t.Fatalf("bearers must be distinct and non-empty: %q/%q", first, second)
	}
	for _, token := range []string{first, second} {
		loginID, err := manager.GetLoginID(token)
		if err != nil || loginID != "42" {
			t.Fatalf("token login ID = %q/%v, want 42/nil", loginID, err)
		}
		roles, err := manager.GetRoles(loginID)
		if err != nil || len(roles) != 1 || roles[0] != "operator" {
			t.Fatalf("shared Roles = %v/%v, want operator", roles, err)
		}
		permissions, err := manager.GetPermissions(loginID)
		if err != nil || len(permissions) != 1 || permissions[0] != "system:user:edit" {
			t.Fatalf("shared Functional Permissions = %v/%v", permissions, err)
		}
	}
}

func TestAuthorizationSnapshotPreservesSnowflakeSystemUserIDExactly(t *testing.T) {
	_, manager, _ := newTestLoginSessionRepo(t)
	const systemUserID int64 = 9_007_199_254_740_993
	_ = issueTestBearer(t, manager, systemUserID, "alice", "desktop")
	session, err := manager.GetSession("9007199254740993")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	got, ok := session.Get(authorizationSnapshotSystemUserIDKey)
	if !ok || got != "9007199254740993" {
		t.Fatalf("stored System User ID = %#v/%v, want exact decimal string", got, ok)
	}
}

func TestAuthorizationSnapshotAddsWildcardOnlyThroughSuperadminRole(t *testing.T) {
	snapshot := toBizAuthorizationSnapshot(42, "alice", []*ent.Role{
		{ID: 1, RoleKey: "viewer"},
		{ID: 2, RoleKey: "superadmin"},
	}, []*ent.Menu{
		{ID: 10, Perms: "system:user:list"},
		{ID: 11, Perms: " system:user:list "},
		{ID: 12, Perms: ""},
	})
	if strings.Join(snapshot.RoleKeys, ",") != "superadmin,viewer" {
		t.Fatalf("Role keys = %v", snapshot.RoleKeys)
	}
	if strings.Join(snapshot.Permissions, ",") != "*:*:*,system:user:list" {
		t.Fatalf("Functional Permissions = %v", snapshot.Permissions)
	}

	withoutWildcard := toBizAuthorizationSnapshot(42, "alice", []*ent.Role{{ID: 1, RoleKey: "viewer"}}, []*ent.Menu{{ID: 13, Perms: "*:*:*"}})
	if len(withoutWildcard.Permissions) != 0 {
		t.Fatalf("wildcard added without superadmin Role: %v", withoutWildcard.Permissions)
	}
	malformedSuperadmin := toBizAuthorizationSnapshot(42, "alice", []*ent.Role{{ID: 2, RoleKey: " superadmin "}}, nil)
	if len(malformedSuperadmin.Permissions) != 0 {
		t.Fatalf("non-exact superadmin Role key granted wildcard: %v", malformedSuperadmin.Permissions)
	}
}

func TestSystemUserAuthorizationLockUsesTransactionScopedPostgresAdvisoryLock(t *testing.T) {
	table := entsql.Table("sys_user")
	selector := entsql.Select(table.C("user_id")).From(table)
	systemUserAuthorizationLock(42)(selector)
	query, args := selector.Query()
	if !strings.Contains(query, "pg_advisory_xact_lock") || !strings.Contains(query, "::text") {
		t.Fatalf("lock query = %q, want transaction-scoped PostgreSQL advisory lock", query)
	}
	if len(args) != 1 || args[0] != "glex:authorization:system-user:42" {
		t.Fatalf("lock args = %#v, want namespaced System User lock key", args)
	}
}

func TestAuthLoginWrapperLocksReloadsSnapshotRegistersAndUpdatesLastLogin(t *testing.T) {
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
	storage := newAuthStorage(rdb, "glex:auth", time.Second, time.Second)
	manager, cleanup, err := NewAuthManager(storage, testAuthConf())
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanup)
	data := &Data{db: client, rdb: rdb, redisReadTimeout: time.Second, redisWriteTimeout: time.Second}
	sessions := NewLoginSessionRepo(data, manager, testAuthConf())
	repo := NewAuthLoginRepo(data, manager, sessions, testAuthConf())

	loginTime := time.Unix(1_700_000_000, 0)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "sys_user".*pg_advisory_xact_lock`).
		WillReturnRows(sqlmock.NewRows([]string{user.FieldID}).AddRow(int64(42)))
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), "alice", "Alice", nil, nil, int8(0), int8(0), "hash", "", "", int64(0), int64(0), "", nil, loginTime, loginTime, nil,
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
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE "user_id"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), "alice", "Alice", nil, nil, int8(0), int8(0), "hash", "", "", int64(0), int64(0), "192.0.2.10", loginTime, loginTime, loginTime, nil,
		))
	mock.ExpectCommit()

	result, err := repo.Issue(t.Context(), &biz.LoginIssue{
		SystemUserID: 42,
		UserName:     "alice",
		PasswordHash: "hash",
		SourceIP:     "192.0.2.10",
		DeviceType:   "desktop",
		Browser:      "Chrome",
		OS:           "Linux",
		LoginTime:    loginTime,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if result.AccessToken == "" || result.ExpiresIn != 7*24*time.Hour {
		t.Fatalf("LoginResult = %#v", result)
	}
	listed, err := sessions.ListBySystemUser(t.Context(), 42)
	if err != nil || len(listed) != 1 {
		t.Fatalf("authoritative Login Sessions = %v/%v", listed, err)
	}
	if listed[0].ID == result.AccessToken || listed[0].Browser != "Chrome" || listed[0].IPAddress != "192.0.2.10" {
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
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestAuthLoginWrapperRevalidatesPasswordUnderLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	data := &Data{db: client}
	repo := &authLoginRepo{data: data}
	now := time.Unix(1_700_000_000, 0)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "sys_user".*pg_advisory_xact_lock`).
		WillReturnRows(sqlmock.NewRows([]string{user.FieldID}).AddRow(int64(42)))
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), "alice", "Alice", nil, nil, int8(0), int8(0), "new-hash", "", "", int64(0), int64(0), "", nil, now, now, nil,
		))
	mock.ExpectRollback()
	_, err = repo.Issue(t.Context(), &biz.LoginIssue{SystemUserID: 42, UserName: "alice", PasswordHash: "old-hash"})
	if !errors.Is(err, biz.ErrInvalidCredentials) {
		t.Fatalf("Issue stale password = %v, want INVALID_CREDENTIALS", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestAuthLoginWrapperMapsDatabaseDriverErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := &authLoginRepo{data: &Data{db: client}}
	mock.ExpectBegin().WillReturnError(errors.New("database unavailable"))

	_, err = repo.Issue(t.Context(), &biz.LoginIssue{
		SystemUserID: 42,
		UserName:     "alice",
		PasswordHash: "hash",
	})
	if !errors.Is(err, biz.ErrAuthBackendUnavailable) {
		t.Fatalf("Issue database error = %v, want ErrAuthBackendUnavailable", err)
	}
	if strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("driver error escaped data boundary: %v", err)
	}
}
