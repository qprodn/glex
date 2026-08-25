package authninfra

import (
	"glex/internal/infra/store"

	"errors"
	"testing"
	"time"

	userapp "glex/internal/application/user"
	authnimpl "glex/internal/crossdomain/authn/impl"
	access "glex/internal/domain/access"
	authndomain "glex/internal/domain/authn"
	userdomain "glex/internal/domain/user"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/user"
	userinfra "glex/internal/infra/user"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type securityIntegration struct {
	data      *store.Data
	client    *ent.Client
	sessions  authndomain.LoginSessionRepo
	users     userdomain.UserRepo
	roles     access.RoleRepo
	menus     access.MenuRepo
	userRoles access.UserRoleRepo
	graph     *authGraph
	seed      *integrationSeeder
}

func newSecurityIntegration(t *testing.T) *securityIntegration {
	t.Helper()
	graph := newIntegrationStores(t).newGraph(t)
	return &securityIntegration{
		data:      graph.data,
		client:    graph.db,
		sessions:  graph.sessions,
		users:     graph.users,
		roles:     graph.roles,
		menus:     graph.menus,
		userRoles: graph.userRoles,
		graph:     graph,
		seed:      newIntegrationSeeder(),
	}
}

func (h *securityIntegration) user(t *testing.T, status int8) int64 {
	t.Helper()
	return h.seed.user(t, h.graph, status)
}

func (h *securityIntegration) role(t *testing.T, status int8) int64 {
	t.Helper()
	return h.seed.role(t, h.graph, status)
}

func (h *securityIntegration) menu(t *testing.T, perms string, status int8) int64 {
	t.Helper()
	return h.seed.menu(t, h.graph, perms, status)
}

func (h *securityIntegration) session(t *testing.T, systemUserID int64) string {
	t.Helper()
	token := issueTestBearer(t, h.graph.manager, systemUserID, integrationUserName(systemUserID), "desktop")
	created, err := h.sessions.Create(t.Context(), token, &authndomain.LoginSession{SystemUserID: systemUserID, UserName: integrationUserName(systemUserID), DeviceType: "desktop", LoginTime: time.Now()})
	if err != nil {
		t.Fatalf("create Login Session: %v", err)
	}
	return created.ID
}

func (h *securityIntegration) requireRevoked(t *testing.T, sessionID string) {
	t.Helper()
	requireRevoked(t, sessionID, h.graph)
}

// TestSecurityChangeTriggersRevokeAffectedSessionsIntegration covers the two
// Usecases whose Redis ordering is a business requirement: Role Assignment
// and Menu Grant replacement.
func TestSecurityChangeTriggersRevokeAffectedSessionsIntegration(t *testing.T) {
	h := newSecurityIntegration(t)

	t.Run("Role Assignment replacement", func(t *testing.T) {
		uid, rid := h.user(t, 0), h.role(t, 0)
		sid := h.session(t, uid)
		if _, err := h.graph.assignmentUC.UpdateUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatalf("UpdateUserRoles: %v", err)
		}
		h.requireRevoked(t, sid)
	})

	t.Run("Menu Grant replacement", func(t *testing.T) {
		uid, rid, mid := h.user(t, 0), h.role(t, 0), h.menu(t, "system:x:list", 0)
		if _, err := h.graph.assignmentUC.UpdateUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatalf("seed Role Assignment: %v", err)
		}
		sid := h.session(t, uid)
		if _, err := h.graph.roleUC.UpdateRoleMenus(t.Context(), rid, []int64{mid}, true); err != nil {
			t.Fatalf("UpdateRoleMenus: %v", err)
		}
		h.requireRevoked(t, sid)
	})

}

func TestRoleAssignmentAndMenuGrantRedisFailureRollsBackIntegration(t *testing.T) {
	t.Run("Role Assignment", func(t *testing.T) {
		h := newSecurityIntegration(t)
		uid, rid := h.user(t, 0), h.role(t, 0)
		_ = h.session(t, uid)
		if err := h.data.Redis.Close(); err != nil {
			t.Fatalf("close Redis client: %v", err)
		}
		if _, err := h.graph.assignmentUC.UpdateUserRoles(t.Context(), uid, []int64{rid}); err == nil {
			t.Fatal("UpdateUserRoles succeeded through injected Redis failure")
		}
		assignments, err := h.userRoles.ListRoleAssignments(t.Context(), uid)
		if err != nil {
			t.Fatalf("ListRoleAssignments: %v", err)
		}
		if len(assignments) != 1 || assignments[0].Assigned {
			t.Fatalf("Role Assignment after Redis failure = %#v", assignments)
		}
	})

	t.Run("Menu Grant", func(t *testing.T) {
		h := newSecurityIntegration(t)
		uid, rid, mid := h.user(t, 0), h.role(t, 0), h.menu(t, "system:x:list", 0)
		if _, err := h.graph.assignmentUC.UpdateUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatalf("seed Role Assignment: %v", err)
		}
		_ = h.session(t, uid)
		if err := h.data.Redis.Close(); err != nil {
			t.Fatalf("close Redis client: %v", err)
		}
		if _, err := h.graph.roleUC.UpdateRoleMenus(t.Context(), rid, []int64{mid}, true); err == nil {
			t.Fatal("UpdateRoleMenus succeeded through injected Redis failure")
		}
		links, err := h.roles.ListRoleMenuLinks(t.Context(), rid)
		if err != nil {
			t.Fatalf("ListRoleMenuLinks: %v", err)
		}
		if len(links) != 0 {
			t.Fatalf("Menu Grant after Redis failure = %#v", links)
		}
	})
}

// TestSecurityChangeCommitFailureKeepsSessionsRevokedIntegration injects a
// PostgreSQL commit failure through sqlmock while the revoker runs against
// real (miniredis) Redis: the mutation must not surface, and the already-run
// revocation deliberately stays — the fail-safe direction of ADR-0010.
func TestSecurityChangeCommitFailureKeepsSessionsRevokedIntegration(t *testing.T) {
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
	manager, cleanupManager, err := NewAuthManager(storage, testAuthConf())
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanupManager)
	data := &store.Data{DB: ent.NewDatabaseFromClient(client), Redis: rdb, Logger: testLogger(), RedisReadTimeout: time.Second, RedisWriteTimeout: time.Second}
	sessions := NewLoginSessionRepo(data, manager, testAuthConf())
	revoker := authnimpl.NewSessionRevoker(sessions)
	tx := store.NewTxManager(data)
	uc := userapp.NewUserUsecase(userinfra.NewUserRepo(data), authnimpl.NewCredentialWriter(NewCredentialRepo(data)), tx, revoker, testLogger())

	token := issueTestBearer(t, manager, 42, "alice", "desktop")
	created, err := sessions.Create(t.Context(), token, &authndomain.LoginSession{SystemUserID: 42, UserName: "alice", DeviceType: "desktop", LoginTime: time.Unix(1_700_000_000, 0)})
	if err != nil {
		t.Fatalf("seed Login Session: %v", err)
	}

	commitFailure := errors.New("injected PostgreSQL commit failure")
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).WillReturnRows(sqlmock.NewRows([]string{"user_id"})) // uniqueness read: available
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), "alice", "Alice", nil, nil, int8(0), int8(0), "hash", "", "", int64(0), int64(0), "", nil, time.Unix(1_700_000_000, 0), time.Unix(1_700_000_000, 0), nil,
		))
	// ent's update-one path executes the UPDATE then re-reads the node.
	mock.ExpectExec(`UPDATE "sys_user"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).
		WillReturnRows(sqlmock.NewRows(user.Columns).AddRow(
			int64(42), "alice", "Alice", nil, nil, int8(1), int8(0), "hash", "", "", int64(0), int64(0), "", nil, time.Unix(1_700_000_000, 0), time.Unix(1_700_000_000, 0), nil,
		))
	mock.ExpectCommit().WillReturnError(commitFailure)

	_, err = uc.UpdateUser(t.Context(), &userdomain.User{ID: 42, UserName: "alice", NickName: "Alice", Status: 1})
	if !errors.Is(err, commitFailure) {
		t.Fatalf("UpdateUser error = %v, want injected commit failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
	if _, err := sessions.FindByID(t.Context(), created.ID); !errors.Is(err, authndomain.ErrLoginSessionNotFound) {
		t.Fatalf("revocation must survive a commit failure (fail-safe direction): %v", err)
	}
}
