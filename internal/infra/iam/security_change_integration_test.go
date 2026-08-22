package iaminfra

import (
	"glex/internal/infra/store"

	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	iamapp "glex/internal/application/iam"
	"glex/internal/domain/iam"
	"glex/internal/infra/ent"
	"glex/internal/infra/ent/user"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type securityIntegration struct {
	data      *store.Data
	client    *ent.Client
	sessions  iam.LoginSessionRepo
	users     iam.UserRepo
	roles     iam.RoleRepo
	menus     iam.MenuRepo
	userRoles iam.UserRoleRepo
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
	created, err := h.sessions.Create(t.Context(), token, &iam.LoginSession{SystemUserID: systemUserID, UserName: integrationUserName(systemUserID), DeviceType: "desktop", LoginTime: time.Now()})
	if err != nil {
		t.Fatalf("create Login Session: %v", err)
	}
	return created.ID
}

func (h *securityIntegration) requireRevoked(t *testing.T, sessionID string) {
	t.Helper()
	requireRevoked(t, sessionID, h.graph)
}

// TestSecurityChangeTriggersRevokeAffectedSessionsIntegration drives the
// wired usecases — the only place revocation pairs with the database
// mutation — and asserts each authorization-invalidating write revokes the
// assigned user's Login Sessions in the same transaction.
func TestSecurityChangeTriggersRevokeAffectedSessionsIntegration(t *testing.T) {
	h := newSecurityIntegration(t)

	t.Run("Role Assignment replacement", func(t *testing.T) {
		uid, rid := h.user(t, 0), h.role(t, 0)
		sid := h.session(t, uid)
		if _, err := h.graph.authUC.UpdateUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatalf("UpdateUserRoles: %v", err)
		}
		h.requireRevoked(t, sid)
	})

	t.Run("Menu Grant replacement", func(t *testing.T) {
		uid, rid, mid := h.user(t, 0), h.role(t, 0), h.menu(t, "system:x:list", 0)
		if _, err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatalf("seed Role Assignment: %v", err)
		}
		sid := h.session(t, uid)
		if _, err := h.graph.roleUC.UpdateRoleMenus(t.Context(), rid, []int64{mid}, true); err != nil {
			t.Fatalf("UpdateRoleMenus: %v", err)
		}
		h.requireRevoked(t, sid)
	})

	for _, tc := range []struct {
		name string
		run  func(*testing.T, *securityIntegration, int64, int64, int64)
	}{
		{"Role role_key", func(t *testing.T, h *securityIntegration, uid, rid, _ int64) {
			current, err := h.client.Role.Get(t.Context(), rid)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.graph.roleUC.UpdateRole(t.Context(), &iam.Role{ID: rid, RoleName: current.RoleName, RoleKey: current.RoleKey + "-changed", Status: 0}); err != nil {
				t.Fatal(err)
			}
		}},
		{"Menu perms", func(t *testing.T, h *securityIntegration, uid, _, mid int64) {
			if _, err := h.graph.menuUC.UpdateMenu(t.Context(), &iam.Menu{ID: mid, MenuName: "changed", MenuType: iam.MenuTypeButton, Perms: "changed", Status: 0}); err != nil {
				t.Fatal(err)
			}
		}},
		{"Menu status", func(t *testing.T, h *securityIntegration, uid, _, mid int64) {
			if _, err := h.graph.menuUC.UpdateMenu(t.Context(), &iam.Menu{ID: mid, MenuName: "changed", MenuType: iam.MenuTypeButton, Perms: "old", Status: 1}); err != nil {
				t.Fatal(err)
			}
		}},
		{"Menu delete", func(t *testing.T, h *securityIntegration, uid, _, mid int64) {
			if err := h.graph.menuUC.DeleteMenu(t.Context(), mid); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid, rid, mid := h.user(t, 0), h.role(t, 0), h.menu(t, "old", 0)
			if _, err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
				t.Fatal(err)
			}
			if _, err := h.roles.ReplaceRoleMenus(t.Context(), rid, []int64{mid}, true); err != nil {
				t.Fatal(err)
			}
			sid := h.session(t, uid)
			tc.run(t, h, uid, rid, mid)
			h.requireRevoked(t, sid)
		})
	}

	// Disabling or deleting a role that still has users assigned is rejected
	// by the usecase guard before any write — no revocation either.
	t.Run("Role disable assigned is rejected without revocation", func(t *testing.T) {
		uid, rid := h.user(t, 0), h.role(t, 0)
		if _, err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatal(err)
		}
		sid := h.session(t, uid)
		if _, err := h.graph.roleUC.UpdateRole(t.Context(), &iam.Role{ID: rid, RoleName: "disabled", RoleKey: "disabled", Status: 1}); !errors.Is(err, iam.ErrRoleAssigned) {
			t.Fatalf("disable assigned role = %v, want ErrRoleAssigned", err)
		}
		if _, err := h.sessions.FindByID(t.Context(), sid); err != nil {
			t.Fatalf("Session was revoked by a rejected write: %v", err)
		}
	})

	for _, tc := range []struct {
		name string
		run  func(*testing.T, *securityIntegration, int64)
	}{
		{"System User status", func(t *testing.T, h *securityIntegration, uid int64) {
			if _, err := h.graph.userUC.UpdateUser(t.Context(), &iam.User{ID: uid, UserName: "changed-" + strconv.FormatInt(uid, 10), NickName: "User", Status: 1}); err != nil {
				t.Fatal(err)
			}
		}},
		{"System User delete", func(t *testing.T, h *securityIntegration, uid int64) {
			if err := h.graph.userUC.DeleteUser(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
		}},
		{"administrative password reset", func(t *testing.T, h *securityIntegration, uid int64) {
			if _, err := h.graph.userUC.UpdateUser(t.Context(), &iam.User{ID: uid, UserName: "changed-" + strconv.FormatInt(uid, 10), NickName: "User", Password: "new-secret", Status: 0}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid := h.user(t, 0)
			sid := h.session(t, uid)
			tc.run(t, h, uid)
			h.requireRevoked(t, sid)
		})
	}
}

func TestSelfPasswordChangePreservesOnlyCurrentSessionIntegration(t *testing.T) {
	h := newSecurityIntegration(t)
	uid := h.user(t, 0)
	current, other := h.session(t, uid), h.session(t, uid)
	ctx := iam.WithPrincipal(t.Context(), iam.NewPrincipal(uid, integrationUserName(uid), current, nil, nil))
	if _, err := h.graph.userUC.UpdateUser(ctx, &iam.User{ID: uid, UserName: integrationUserName(uid), NickName: "User", Password: "new-secret", Status: 0}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, err := h.sessions.FindByID(t.Context(), current); err != nil {
		t.Fatalf("current Session was revoked: %v", err)
	}
	h.requireRevoked(t, other)
}

func TestSecurityChangeRedisFailureRollsBackIntegration(t *testing.T) {
	h := newSecurityIntegration(t)
	uid := h.user(t, 0)
	_ = h.session(t, uid)
	// Closing the real client injects an actual Redis backend failure into the
	// production Lua revoker, rather than substituting a fake revocation repo.
	if err := h.data.Redis.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
	if _, err := h.graph.userUC.UpdateUser(t.Context(), &iam.User{ID: uid, UserName: "changed-" + strconv.FormatInt(uid, 10), NickName: "User", Status: 1}); err == nil {
		t.Fatal("UpdateUser succeeded through injected Redis failure")
	}
	got, err := h.client.User.Get(t.Context(), uid)
	if err != nil || got.Status != 0 {
		t.Fatalf("PostgreSQL state after Redis failure = %#v/%v", got, err)
	}
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
	revoker := NewSessionRevoker(sessions)
	tx := store.NewTxManager(data)
	uc := iamapp.NewUserUsecase(NewUserRepo(data), tx, revoker, testLogger())

	token := issueTestBearer(t, manager, 42, "alice", "desktop")
	created, err := sessions.Create(t.Context(), token, &iam.LoginSession{SystemUserID: 42, UserName: "alice", DeviceType: "desktop", LoginTime: time.Unix(1_700_000_000, 0)})
	if err != nil {
		t.Fatalf("seed Login Session: %v", err)
	}

	commitFailure := errors.New("injected PostgreSQL commit failure")
	mock.ExpectQuery(`SELECT .* FROM "sys_user"`).WillReturnRows(sqlmock.NewRows([]string{"user_id"})) // uniqueness read: available
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "sys_user".*pg_advisory_xact_lock`).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(int64(42)))
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

	_, err = uc.UpdateUser(t.Context(), &iam.User{ID: 42, UserName: "alice", NickName: "Alice", Status: 1})
	if !errors.Is(err, commitFailure) {
		t.Fatalf("UpdateUser error = %v, want injected commit failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
	if _, err := sessions.FindByID(t.Context(), created.ID); !errors.Is(err, iam.ErrLoginSessionNotFound) {
		t.Fatalf("revocation must survive a commit failure (fail-safe direction): %v", err)
	}
}

func raceLoginWithSecurityChange(t *testing.T, h *securityIntegration, uid int64, change func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	manager := h.sessions.(*loginSessionRepo).manager
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		_, _, err := issueOnGraph(ctx, h.graph, uid, "desktop")
		errs <- err
	}()
	go func() {
		<-start
		errs <- change(ctx)
	}()
	close(start)
	for range 2 {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("concurrent operation: %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("concurrent login/security change deadlocked: %v", ctx.Err())
		}
	}
	sessions, err := h.sessions.ListBySystemUser(t.Context(), uid)
	if err != nil {
		t.Fatalf("list Sessions: %v", err)
	}
	if len(sessions) > 0 {
		userName := integrationUserName(uid)
		expected, err := loadAuthorizationSnapshot(t.Context(), h.data.DB, uid, userName)
		if err != nil {
			t.Fatalf("load final snapshot: %v", err)
		}
		permissions, err := manager.GetPermissions(strconv.FormatInt(uid, 10))
		if err != nil || !reflect.DeepEqual(permissions, expected.Permissions) {
			t.Fatalf("valid concurrent Session has stale snapshot: Sessions=%v permissions=%v expected=%v err=%v", sessions, permissions, expected.Permissions, err)
		}
	}
}

func TestConcurrentLoginAndEachSecurityRelationshipChangeLeaveNoStaleSessionIntegration(t *testing.T) {
	t.Run("Role Assignment", func(t *testing.T) {
		h := newSecurityIntegration(t)
		uid := h.user(t, 0)
		oldRole, newRole := h.role(t, 0), h.role(t, 0)
		oldMenu, newMenu := h.menu(t, "old", 0), h.menu(t, "new", 0)
		if _, err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{oldRole}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.roles.ReplaceRoleMenus(t.Context(), oldRole, []int64{oldMenu}, true); err != nil {
			t.Fatal(err)
		}
		if _, err := h.roles.ReplaceRoleMenus(t.Context(), newRole, []int64{newMenu}, true); err != nil {
			t.Fatal(err)
		}
		raceLoginWithSecurityChange(t, h, uid, func(ctx context.Context) error {
			_, err := h.graph.authUC.UpdateUserRoles(ctx, uid, []int64{newRole})
			return err
		})
	})

	t.Run("Menu Grant", func(t *testing.T) {
		h := newSecurityIntegration(t)
		uid, rid := h.user(t, 0), h.role(t, 0)
		oldMenu, newMenu := h.menu(t, "old", 0), h.menu(t, "new", 0)
		if _, err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.roles.ReplaceRoleMenus(t.Context(), rid, []int64{oldMenu}, true); err != nil {
			t.Fatal(err)
		}
		raceLoginWithSecurityChange(t, h, uid, func(ctx context.Context) error {
			_, err := h.graph.roleUC.UpdateRoleMenus(ctx, rid, []int64{newMenu}, true)
			return err
		})
	})

	t.Run("Menu permission", func(t *testing.T) {
		h := newSecurityIntegration(t)
		uid, rid, mid := h.user(t, 0), h.role(t, 0), h.menu(t, "old", 0)
		if _, err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.roles.ReplaceRoleMenus(t.Context(), rid, []int64{mid}, true); err != nil {
			t.Fatal(err)
		}
		raceLoginWithSecurityChange(t, h, uid, func(ctx context.Context) error {
			_, err := h.graph.menuUC.UpdateMenu(ctx, &iam.Menu{ID: mid, MenuName: "menu", MenuType: iam.MenuTypeButton, Perms: "new", Status: 0})
			return err
		})
	})
}

func TestAuthorizationLockOrderDoesNotDeadlockIntegration(t *testing.T) {
	h := newSecurityIntegration(t)
	menus := []int64{h.menu(t, "one", 0), h.menu(t, "two", 0)}
	roles := []int64{h.role(t, 0), h.role(t, 0)}
	users := []int64{h.user(t, 0), h.user(t, 0)}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	errs := make(chan error, 2)
	lock := func(menuIDs, roleIDs, userIDs []int64) {
		<-start
		errs <- h.graph.tx.InTx(ctx, func(ctx context.Context) error {
			if err := lockMenus(ctx, h.data.DB, menuIDs...); err != nil {
				return err
			}
			if err := lockRoles(ctx, h.data.DB, roleIDs...); err != nil {
				return err
			}
			return lockSystemUsers(ctx, h.data.DB, userIDs...)
		})
	}
	go lock([]int64{menus[1], menus[0]}, []int64{roles[1], roles[0]}, []int64{users[1], users[0]})
	go lock([]int64{menus[0], menus[1]}, []int64{roles[0], roles[1]}, []int64{users[0], users[1]})
	close(start)
	for range 2 {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("lock transaction: %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("global authorization lock order deadlocked: %v", ctx.Err())
		}
	}
}
