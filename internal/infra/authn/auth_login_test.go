package authninfra

import (
	"glex/internal/infra/store"

	"context"
	"errors"
	"strings"
	"testing"
	"time"

	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnrepo "glex/internal/domain/authn/repository"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/user"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core"
)

func issueTestBearer(t *testing.T, manager *core.Manager, systemUserID int64, userName, deviceType string) string {
	t.Helper()
	token, err := loginAndReplaceAuthorizationSnapshot(manager, &authnentity.AuthorizationSnapshot{
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
	first, err := loginAndReplaceAuthorizationSnapshot(manager, &authnentity.AuthorizationSnapshot{
		SystemUserID: 42,
		UserName:     "alice",
		RoleKeys:     []string{"viewer"},
		Permissions:  []string{"system:user:list"},
	}, "desktop", 7*24*time.Hour)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	second, err := loginAndReplaceAuthorizationSnapshot(manager, &authnentity.AuthorizationSnapshot{
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
			t.Fatalf("shared Functional Permissions = %v/%v, want operator", permissions, err)
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

// newAuthLoginSQLMock builds an authLoginRepo over sqlmock PostgreSQL and
// miniredis-backed sa-token/session stores.
func newAuthLoginSQLMock(t *testing.T) (*authLoginRepo, *core.Manager, authnrepo.LoginSessionRepo, sqlmock.Sqlmock) {
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
	storage := newAuthStorage(rdb, "glex:auth", time.Second, time.Second, testLogger())
	manager, cleanup, err := NewAuthManager(storage, testAuthConf())
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanup)
	data := &store.Data{DB: ent.NewDatabaseFromClient(client), Redis: rdb, Logger: testLogger(), RedisReadTimeout: time.Second, RedisWriteTimeout: time.Second}
	sessions := NewLoginSessionRepo(data, manager, testAuthConf())
	return &authLoginRepo{data: data, manager: manager, sessions: sessions, fixedLifetime: 7 * 24 * time.Hour}, manager, sessions, mock
}

func TestAuthLoginRevalidateRejectsStaleHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := &authLoginRepo{data: &store.Data{DB: ent.NewDatabaseFromClient(client)}, logger: testLogger()}
	now := time.Unix(1_700_000_000, 0)

	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), int64(0), int64(0), now, now, nil, "alice", "Alice", nil, nil, int8(0), int8(0), "new-hash", "", "", "", nil,
		))
	_, err = repo.Revalidate(t.Context(), 42, "alice", "old-hash")
	if !errors.Is(err, authndomain.ErrInvalidCredentials) {
		t.Fatalf("stale password = %v, want INVALID_CREDENTIALS", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestAuthLoginMapsDatabaseDriverErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := &authLoginRepo{data: &store.Data{DB: ent.NewDatabaseFromClient(client)}, logger: testLogger()}
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).WillReturnError(errors.New("database unavailable"))

	_, err = repo.Revalidate(t.Context(), 42, "alice", "hash")
	if !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("database error = %v, want ErrAuthBackendUnavailable", err)
	}
	if strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("driver error escaped data boundary: %v", err)
	}
}

// stubFailingSessions is a LoginSessionRepo whose Create fails; every other
// method lives on the embedded nil interface because IssueCredential's
// failure path only calls Create and ListBySystemUser.
type stubFailingSessions struct {
	authnrepo.LoginSessionRepo
	createErr error
}

func (s *stubFailingSessions) Create(context.Context, string, *authnentity.LoginSession) (*authnentity.LoginSession, error) {
	return nil, s.createErr
}

func (s *stubFailingSessions) ListBySystemUser(context.Context, int64, ...authnrepo.ListOption) ([]*authnentity.LoginSession, error) {
	return nil, nil
}

func TestAuthLoginIssueCredentialCleansUpPartialRedisFailure(t *testing.T) {
	repo, manager, _, _ := newAuthLoginSQLMock(t)
	repo.sessions = &stubFailingSessions{createErr: errors.New("session store unavailable")}

	result, err := repo.IssueCredential(t.Context(), &authnentity.AuthorizationSnapshot{SystemUserID: 42, UserName: "alice", RoleKeys: []string{"viewer"}}, &authnentity.LoginSession{
		SystemUserID: 42,
		UserName:     "alice",
		DeviceType:   "desktop",
	})
	if err == nil || result != nil {
		t.Fatalf("partial Redis failure = %v/%#v, want error/nil", err, result)
	}
	// sa-token recreates an empty Account Session on read; the proof of
	// cleanup is that the snapshot the failed issuance wrote is gone.
	session, err := manager.GetSession("42")
	if err != nil {
		t.Fatalf("GetSession after cleanup: %v", err)
	}
	if _, ok := session.Get(authorizationSnapshotSystemUserIDKey); ok {
		t.Fatal("Account Session snapshot survived a failed issuance")
	}
}

func TestAuthLoginDiscardCredentialTearsDownTokenAndLastSession(t *testing.T) {
	repo, manager, sessions, _ := newAuthLoginSQLMock(t)
	snapshot := &authnentity.AuthorizationSnapshot{SystemUserID: 42, UserName: "alice", RoleKeys: []string{"viewer"}}
	result, err := repo.IssueCredential(t.Context(), snapshot, &authnentity.LoginSession{
		SystemUserID: 42, UserName: "alice", DeviceType: "desktop", LoginTime: time.Unix(1_700_000_000, 0),
	})
	if err != nil {
		t.Fatalf("IssueCredential: %v", err)
	}

	if err := repo.DiscardCredential(t.Context(), 42, result.AccessToken); err != nil {
		t.Fatalf("DiscardCredential: %v", err)
	}
	listed, err := sessions.ListBySystemUser(t.Context(), 42)
	if err != nil || len(listed) != 0 {
		t.Fatalf("Login Sessions after discard = %v/%v, want none", listed, err)
	}
	if _, err := manager.GetLoginID(result.AccessToken); err == nil {
		t.Fatal("bearer token must be destroyed after discard")
	}
	// Discard is idempotent.
	if err := repo.DiscardCredential(t.Context(), 42, result.AccessToken); err != nil {
		t.Fatalf("DiscardCredential (repeat): %v", err)
	}
}
