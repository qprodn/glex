package integration

import (
	"errors"
	"testing"
	"time"

	userapp "glex/internal/application/user"
	authnimpl "glex/internal/crossdomain/authn/impl"
	access "glex/internal/domain/access"
	authndomain "glex/internal/domain/authn"
	userdomain "glex/internal/domain/user"
	authninfra "glex/internal/infra/authn"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/user"
	"glex/internal/infra/store"
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
	// The production credential issuer (not a test-only mint): manager login,
	// snapshot replacement, and authoritative registration in one call.
	result, err := h.graph.login.IssueCredential(t.Context(), &authndomain.AuthorizationSnapshot{
		SystemUserID: systemUserID,
		UserName:     integrationUserName(systemUserID),
	}, &authndomain.LoginSession{SystemUserID: systemUserID, UserName: integrationUserName(systemUserID), DeviceType: "desktop", LoginTime: time.Now()})
	if err != nil {
		t.Fatalf("issue Login Session: %v", err)
	}
	created, err := h.sessions.FindByToken(t.Context(), result.AccessToken)
	if err != nil {
		t.Fatalf("load issued Login Session: %v", err)
	}
	return created.ID
}

func (h *securityIntegration) requireRevoked(t *testing.T, sessionID string) {
	t.Helper()
	requireRevoked(t, sessionID, h.graph)
}

// TestSecurityChangeTriggersRevokeAffectedSessionsIntegration covers the two
// Usecases whose Redis ordering is a business requirement: Role Assignment
// and Menu Grant replacement. Both are cross-module security changes: the
// access Usecases revoke through the authn contract while their PostgreSQL
// transaction is still open.
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
		// ListRoleAssignments lists every role in the shared database, so the
		// rollback is asserted relative to this test's own seed: the role is
		// present but unassigned, and nothing at all is assigned to the
		// (globally unique) System User.
		assignments, err := h.userRoles.ListRoleAssignments(t.Context(), uid)
		if err != nil {
			t.Fatalf("ListRoleAssignments: %v", err)
		}
		seeded := false
		for _, assignment := range assignments {
			if assignment.Assigned {
				t.Fatalf("Role Assignment after Redis failure left %q assigned", assignment.Role.RoleKey)
			}
			if assignment.Role.ID == rid {
				seeded = true
			}
		}
		if !seeded {
			t.Fatalf("seeded role %d missing from Role Assignments", rid)
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
// revocation deliberately stays — the fail-safe direction of ADR-0010. The
// graph is composed from the modules' exported constructors only, proving the
// cross-module seam (user Usecase → authn revocation contract) needs no
// test-only wiring.
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
	conf := testAuthConf()
	data := &store.Data{DB: ent.NewDatabaseFromClient(client), Redis: rdb, Logger: testLogger(), RedisReadTimeout: time.Second, RedisWriteTimeout: time.Second}
	manager, cleanupManager, err := authninfra.NewAuthManager(authninfra.NewAuthStorage(data, conf), conf)
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanupManager)
	sessions := authninfra.NewLoginSessionRepo(data, manager, conf)
	login := authninfra.NewAuthLoginBackend(data, manager, sessions, conf)
	revoker := authnimpl.NewSessionRevoker(sessions)
	tx := store.NewTxManager(data)
	uc := userapp.NewUserUsecase(userinfra.NewUserRepo(data), authnimpl.NewCredentialWriter(authninfra.NewCredentialRepo(data)), tx, revoker, testLogger())

	issued, err := login.IssueCredential(t.Context(), &authndomain.AuthorizationSnapshot{SystemUserID: 42, UserName: "alice"}, &authndomain.LoginSession{SystemUserID: 42, UserName: "alice", DeviceType: "desktop", LoginTime: time.Unix(1_700_000_000, 0)})
	if err != nil {
		t.Fatalf("seed Login Session: %v", err)
	}
	created, err := sessions.FindByToken(t.Context(), issued.AccessToken)
	if err != nil {
		t.Fatalf("load seeded Login Session: %v", err)
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
