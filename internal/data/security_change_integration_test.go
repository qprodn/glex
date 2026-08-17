package data

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"glex/internal/biz"
	"glex/internal/data/ent"
)

type securityIntegration struct {
	data        *Data
	client      *ent.Client
	sessions    biz.LoginSessionRepo
	users       biz.UserRepo
	roles       biz.RoleRepo
	menus       biz.MenuRepo
	userRoles   biz.UserRoleRepo
	coordinator *securityChangeCoordinator
	graph       *authGraph
	seed        *integrationSeeder
}

func newSecurityIntegration(t *testing.T) *securityIntegration {
	t.Helper()
	graph := newIntegrationStores(t).newGraph(t)
	return &securityIntegration{
		data:        graph.data,
		client:      graph.db,
		sessions:    graph.sessions,
		users:       graph.users,
		roles:       graph.roles,
		menus:       graph.menus,
		userRoles:   graph.userRoles,
		coordinator: graph.coordinator,
		graph:       graph,
		seed:        newIntegrationSeeder(),
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
	created, err := h.sessions.Create(t.Context(), token, &biz.LoginSession{SystemUserID: systemUserID, UserName: integrationUserName(systemUserID), DeviceType: "desktop", LoginTime: time.Now()})
	if err != nil {
		t.Fatalf("create Login Session: %v", err)
	}
	return created.ID
}

func (h *securityIntegration) requireRevoked(t *testing.T, sessionID string) {
	t.Helper()
	requireRevoked(t, sessionID, h.graph)
}

func TestSecurityChangeTriggersRevokeAffectedSessionsIntegration(t *testing.T) {
	h := newSecurityIntegration(t)

	t.Run("Role Assignment replacement", func(t *testing.T) {
		uid, rid := h.user(t, 0), h.role(t, 0)
		sid := h.session(t, uid)
		if err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatalf("ReplaceUserRoles: %v", err)
		}
		h.requireRevoked(t, sid)
	})

	t.Run("Menu Grant replacement", func(t *testing.T) {
		uid, rid, mid := h.user(t, 0), h.role(t, 0), h.menu(t, "system:x:list", 0)
		if err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatalf("seed Role Assignment: %v", err)
		}
		sid := h.session(t, uid)
		if err := h.roles.ReplaceRoleMenus(t.Context(), rid, []int64{mid}, true); err != nil {
			t.Fatalf("ReplaceRoleMenus: %v", err)
		}
		h.requireRevoked(t, sid)
	})

	for _, tc := range []struct {
		name string
		run  func(*testing.T, *securityIntegration, int64, int64, int64)
	}{
		{"Role role_key", func(t *testing.T, h *securityIntegration, uid, rid, _ int64) {
			_, err := h.roles.UpdateRole(t.Context(), &biz.Role{ID: rid, RoleName: "changed", RoleKey: "changed", Status: 0})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"Role status", func(t *testing.T, h *securityIntegration, uid, rid, _ int64) {
			current, err := h.client.Role.Get(t.Context(), rid)
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.roles.UpdateRole(t.Context(), &biz.Role{ID: rid, RoleName: current.RoleName, RoleKey: current.RoleKey, Status: 1})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"Role delete", func(t *testing.T, h *securityIntegration, uid, rid, _ int64) {
			if err := h.roles.DeleteRole(t.Context(), rid); err != nil {
				t.Fatal(err)
			}
		}},
		{"Menu perms", func(t *testing.T, h *securityIntegration, uid, _, mid int64) {
			_, err := h.menus.UpdateMenu(t.Context(), &biz.Menu{ID: mid, MenuName: "changed", MenuType: biz.MenuTypeButton, Perms: "changed", Status: 0})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"Menu status", func(t *testing.T, h *securityIntegration, uid, _, mid int64) {
			_, err := h.menus.UpdateMenu(t.Context(), &biz.Menu{ID: mid, MenuName: "changed", MenuType: biz.MenuTypeButton, Perms: "old", Status: 1})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"Menu delete", func(t *testing.T, h *securityIntegration, uid, _, mid int64) {
			if err := h.menus.DeleteMenu(t.Context(), mid); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid, rid, mid := h.user(t, 0), h.role(t, 0), h.menu(t, "old", 0)
			if err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
				t.Fatal(err)
			}
			if err := h.roles.ReplaceRoleMenus(t.Context(), rid, []int64{mid}, true); err != nil {
				t.Fatal(err)
			}
			sid := h.session(t, uid)
			tc.run(t, h, uid, rid, mid)
			h.requireRevoked(t, sid)
		})
	}

	for _, tc := range []struct {
		name string
		run  func(*testing.T, *securityIntegration, int64)
	}{
		{"System User status", func(t *testing.T, h *securityIntegration, uid int64) {
			_, err := h.users.UpdateUser(t.Context(), &biz.User{ID: uid, UserName: "changed", NickName: "User", Status: 1}, biz.RevokeAllLoginSessions())
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"System User delete", func(t *testing.T, h *securityIntegration, uid int64) {
			if err := h.users.DeleteUser(t.Context(), uid); err != nil {
				t.Fatal(err)
			}
		}},
		{"administrative password reset", func(t *testing.T, h *securityIntegration, uid int64) {
			_, err := h.users.UpdateUser(t.Context(), &biz.User{ID: uid, UserName: "changed", NickName: "User", Password: "new-hash", Status: 0}, biz.RevokeAllLoginSessions())
			if err != nil {
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
	_, err := h.users.UpdateUser(t.Context(), &biz.User{ID: uid, UserName: "changed", NickName: "User", Password: "new-hash", Status: 0}, biz.PreserveLoginSession(current))
	if err != nil {
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
	if err := h.data.rdb.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
	if _, err := h.users.UpdateUser(t.Context(), &biz.User{ID: uid, UserName: "changed", NickName: "User", Status: 1}, biz.RevokeAllLoginSessions()); err == nil {
		t.Fatal("UpdateUser succeeded through injected Redis failure")
	}
	got, err := h.client.User.Get(t.Context(), uid)
	if err != nil || got.Status != 0 {
		t.Fatalf("PostgreSQL state after Redis failure = %#v/%v", got, err)
	}
}

func TestSecurityChangeCommitFailureLeavesPostgresUnchangedAndRedisRevokedIntegration(t *testing.T) {
	h := newSecurityIntegration(t)
	uid := h.user(t, 0)
	sid := h.session(t, uid)
	coordinator := NewSecurityChangeCoordinator(h.data, h.sessions)
	commitFailure := errors.New("injected PostgreSQL commit failure")
	coordinator.commit = func(*ent.Tx) error { return commitFailure }
	repo := NewUserRepo(h.data, coordinator)

	_, err := repo.UpdateUser(t.Context(), &biz.User{ID: uid, UserName: "changed", NickName: "User", Status: 1}, biz.RevokeAllLoginSessions())
	if !errors.Is(err, commitFailure) {
		t.Fatalf("UpdateUser error = %v, want injected commit failure", err)
	}
	got, err := h.client.User.Get(t.Context(), uid)
	if err != nil || got.Status != 0 {
		t.Fatalf("PostgreSQL state after commit failure = %#v/%v", got, err)
	}
	h.requireRevoked(t, sid)
}

func raceLoginWithSecurityChange(t *testing.T, h *securityIntegration, uid int64, change func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	manager := h.sessions.(*loginSessionRepo).manager
	loginRepo := NewAuthLoginRepo(h.data, manager, h.sessions, testAuthConf())
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		_, err := loginRepo.Issue(ctx, &biz.LoginIssue{
			SystemUserID: uid,
			UserName:     "user-" + strconv.FormatInt(uid, 10),
			PasswordHash: "hash",
			DeviceType:   "desktop",
			LoginTime:    time.Now(),
		})
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
		tx, err := h.client.Tx(t.Context())
		if err != nil {
			t.Fatalf("begin final snapshot read: %v", err)
		}
		userName := "user-" + strconv.FormatInt(uid, 10)
		expected, err := loadAuthorizationSnapshot(t.Context(), tx, uid, userName)
		_ = tx.Rollback()
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
		if err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{oldRole}); err != nil {
			t.Fatal(err)
		}
		if err := h.roles.ReplaceRoleMenus(t.Context(), oldRole, []int64{oldMenu}, true); err != nil {
			t.Fatal(err)
		}
		if err := h.roles.ReplaceRoleMenus(t.Context(), newRole, []int64{newMenu}, true); err != nil {
			t.Fatal(err)
		}
		raceLoginWithSecurityChange(t, h, uid, func(ctx context.Context) error {
			return h.userRoles.ReplaceUserRoles(ctx, uid, []int64{newRole})
		})
	})

	t.Run("Menu Grant", func(t *testing.T) {
		h := newSecurityIntegration(t)
		uid, rid := h.user(t, 0), h.role(t, 0)
		oldMenu, newMenu := h.menu(t, "old", 0), h.menu(t, "new", 0)
		if err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatal(err)
		}
		if err := h.roles.ReplaceRoleMenus(t.Context(), rid, []int64{oldMenu}, true); err != nil {
			t.Fatal(err)
		}
		raceLoginWithSecurityChange(t, h, uid, func(ctx context.Context) error {
			return h.roles.ReplaceRoleMenus(ctx, rid, []int64{newMenu}, true)
		})
	})

	t.Run("Menu permission", func(t *testing.T) {
		h := newSecurityIntegration(t)
		uid, rid, mid := h.user(t, 0), h.role(t, 0), h.menu(t, "old", 0)
		if err := h.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{rid}); err != nil {
			t.Fatal(err)
		}
		if err := h.roles.ReplaceRoleMenus(t.Context(), rid, []int64{mid}, true); err != nil {
			t.Fatal(err)
		}
		raceLoginWithSecurityChange(t, h, uid, func(ctx context.Context) error {
			_, err := h.menus.UpdateMenu(ctx, &biz.Menu{ID: mid, MenuName: "menu", MenuType: biz.MenuTypeButton, Perms: "new", Status: 0})
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
		errs <- h.coordinator.Run(ctx, func(change *securityChangeTx) error {
			if err := change.LockMenus(ctx, menuIDs...); err != nil {
				return err
			}
			if err := change.LockRoles(ctx, roleIDs...); err != nil {
				return err
			}
			return change.LockSystemUsers(ctx, userIDs...)
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
