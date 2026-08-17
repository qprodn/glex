package data

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	"glex/internal/biz"
	"glex/internal/conf"

	satokensession "github.com/sa-tokens/sa-token-go/core/session"
)

// crossInstanceIntegration runs two independent provider graphs — separate
// PostgreSQL and Redis clients, separate Managers — over the same stores, the
// way two deployed service instances share Session state. Everything these
// tests assert must therefore hold through Redis and PostgreSQL alone.
type crossInstanceIntegration struct {
	stores *integrationStores
	graphs []*authGraph
	seed   *integrationSeeder
}

func newCrossInstanceIntegration(t *testing.T, configure ...func(*conf.Auth)) *crossInstanceIntegration {
	t.Helper()
	stores := newIntegrationStores(t, configure...)
	return &crossInstanceIntegration{
		stores: stores,
		graphs: []*authGraph{stores.newGraph(t), stores.newGraph(t)},
		seed:   newIntegrationSeeder(),
	}
}

// accountSessionKey returns the raw Redis key of the shared Account Session,
// used to assert cleanup on the last Logout.
func (h *crossInstanceIntegration) accountSessionKey(systemUserID int64) string {
	return h.stores.rawKey(authManagerKeyPrefix + satokensession.AccountSessionKeyPrefix + strconv.FormatInt(systemUserID, 10))
}

// requireSessionsOnEveryGraph asserts the authoritative index agrees on every
// instance and returns the first graph's view.
func (h *crossInstanceIntegration) requireSessionsOnEveryGraph(t *testing.T, systemUserID int64, want int) []*biz.LoginSession {
	t.Helper()
	var first []*biz.LoginSession
	for i, g := range h.graphs {
		listed, err := g.sessions.ListBySystemUser(t.Context(), systemUserID)
		if err != nil {
			t.Fatalf("graph %d ListBySystemUser: %v", i, err)
		}
		if len(listed) != want {
			t.Fatalf("graph %d lists %d Sessions, want %d: %+v", i, len(listed), want, listed)
		}
		if i == 0 {
			first = listed
		}
	}
	return first
}

func (h *crossInstanceIntegration) authenticate(t *testing.T, g *authGraph, token string) *biz.Principal {
	t.Helper()
	principal, err := g.authorizer.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return principal
}

// TestTwoInstancesIssueDistinctConcurrentLogins proves concurrent logins
// spread across two instances produce distinct usable credentials, and that
// either instance can authenticate a credential the other one issued.
func TestTwoInstancesIssueDistinctConcurrentLoginsIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	uid := h.seed.user(t, h.graphs[0], 0)

	const logins = 4
	type outcome struct {
		session *biz.LoginSession
		token   string
		err     error
	}
	outcomes := make(chan outcome, logins)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(logins)
	for i := range logins {
		graph := h.graphs[i%len(h.graphs)]
		go func() {
			ready.Done()
			<-start
			session, token, err := issueOnGraph(t.Context(), graph, uid, "desktop")
			outcomes <- outcome{session: session, token: token, err: err}
		}()
	}
	ready.Wait()
	close(start)

	tokens := make(map[string]*biz.LoginSession, logins)
	for range logins {
		result := <-outcomes
		if result.err != nil {
			t.Fatalf("concurrent login: %v", result.err)
		}
		tokens[result.token] = result.session
	}
	if len(tokens) != logins {
		t.Fatalf("%d concurrent logins produced %d distinct tokens", logins, len(tokens))
	}
	h.requireSessionsOnEveryGraph(t, uid, logins)

	for token, session := range tokens {
		for i, g := range h.graphs {
			principal := h.authenticate(t, g, token)
			if principal.SystemUserID() != uid || principal.UserName() != integrationUserName(uid) || principal.SessionID() != session.ID {
				t.Fatalf("graph %d authenticated token as (%d, %q, %q), want (%d, %q, %q)",
					i, principal.SystemUserID(), principal.UserName(), principal.SessionID(), uid, integrationUserName(uid), session.ID)
			}
		}
	}
}

// TestTwoInstancesKeepSameDeviceSessionsDistinct proves two same-device
// logins — the exact case sa-token's account/device mapping overwrites — stay
// independently enumerable, addressable, and authenticated on both instances.
func TestTwoInstancesKeepSameDeviceSessionsDistinctIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	uid := h.seed.user(t, h.graphs[0], 0)

	first, firstToken := loginOnGraph(t, h.graphs[0], uid, "desktop")
	second, secondToken := loginOnGraph(t, h.graphs[1], uid, "desktop")
	if first.ID == second.ID {
		t.Fatal("same-device logins on two instances produced one Session")
	}

	listed := h.requireSessionsOnEveryGraph(t, uid, 2)
	if listed[0].ID != first.ID || listed[1].ID != second.ID {
		t.Fatalf("registration order = [%q, %q], want [%q, %q]", listed[0].ID, listed[1].ID, first.ID, second.ID)
	}
	for i, g := range h.graphs {
		lookedUp, err := g.sessions.FindByToken(t.Context(), firstToken)
		if err != nil || lookedUp.ID != first.ID {
			t.Fatalf("graph %d FindByToken(first) = (%q, %v), want %q", i, lookedUp.ID, err, first.ID)
		}
		lookedUp, err = g.sessions.FindByToken(t.Context(), secondToken)
		if err != nil || lookedUp.ID != second.ID {
			t.Fatalf("graph %d FindByToken(second) = (%q, %v), want %q", i, lookedUp.ID, err, second.ID)
		}
	}
}

// TestSecondInstanceLoginRebuildsSharedAuthorizationSnapshot proves a login on
// one instance rebuilds the single shared Account Snapshot from current
// PostgreSQL state after Manager.Login wipes it — and that every instance, and
// every older credential, immediately serves the rebuilt snapshot.
func TestSecondInstanceLoginRebuildsSharedAuthorizationSnapshotIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	g := h.graphs[0]
	uid := h.seed.user(t, g, 0)
	firstRole := h.seed.role(t, g, 0)
	firstMenu := h.seed.menu(t, g, "one:perm", 0)
	if err := g.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{firstRole}); err != nil {
		t.Fatalf("seed first Role Assignment: %v", err)
	}
	if err := g.roles.ReplaceRoleMenus(t.Context(), firstRole, []int64{firstMenu}, true); err != nil {
		t.Fatalf("seed first Menu Grant: %v", err)
	}

	_, firstToken := loginOnGraph(t, h.graphs[0], uid, "desktop")
	principal := h.authenticate(t, h.graphs[0], firstToken)
	if len(principal.RoleKeys()) != 1 || len(principal.Permissions()) != 1 {
		t.Fatalf("first snapshot = %v/%v, want exactly one Role and one permission", principal.RoleKeys(), principal.Permissions())
	}

	// The second Role Assignment is written directly so the intermediate
	// state differs; the rebuild must read current PostgreSQL truth, not the
	// previous snapshot.
	secondRole := h.seed.role(t, g, 0)
	secondMenu := h.seed.menu(t, g, "two:perm", 0)
	if err := g.roles.ReplaceRoleMenus(t.Context(), secondRole, []int64{secondMenu}, true); err != nil {
		t.Fatalf("seed second Menu Grant: %v", err)
	}
	if _, err := g.db.User.UpdateOneID(uid).AddRoleIDs(secondRole).Save(t.Context()); err != nil {
		t.Fatalf("add second Role Assignment: %v", err)
	}

	_, secondToken := loginOnGraph(t, h.graphs[1], uid, "mobile")
	for _, token := range []string{firstToken, secondToken} {
		for i, graph := range h.graphs {
			principal := h.authenticate(t, graph, token)
			if len(principal.RoleKeys()) != 2 || len(principal.Permissions()) != 2 {
				t.Fatalf("graph %d rebuilt snapshot for token = %v/%v, want both Roles and both permissions",
					i, principal.RoleKeys(), principal.Permissions())
			}
		}
	}
}

// TestCrossInstanceLogoutAndKickoutRevokesEverywhere proves revocation
// issued on one instance takes effect on the other, keeps the distinct 401
// reasons, and deletes the shared Account Session only on the last Logout.
func TestCrossInstanceLogoutAndKickoutRevokesEverywhereIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	uid := h.seed.user(t, h.graphs[0], 0)
	first, firstToken := loginOnGraph(t, h.graphs[0], uid, "desktop")
	second, secondToken := loginOnGraph(t, h.graphs[1], uid, "mobile")

	// Logout runs on the instance that did NOT issue the Session.
	if err := h.graphs[1].sessions.Logout(t.Context(), first.ID); err != nil {
		t.Fatalf("cross-instance Logout: %v", err)
	}
	requireRevoked(t, first.ID, h.graphs...)
	if _, err := h.graphs[0].sessions.FindByToken(t.Context(), firstToken); err == nil {
		t.Fatal("logged-out token still authenticates on the issuing instance")
	}
	principal := h.authenticate(t, h.graphs[0], secondToken)
	if principal.SessionID() != second.ID {
		t.Fatalf("remaining Session = %q, want %q", principal.SessionID(), second.ID)
	}

	// Kickout runs on the other instance again, revoking the last Session.
	if err := h.graphs[0].sessions.Kickout(t.Context(), second.ID); err != nil {
		t.Fatalf("cross-instance Kickout: %v", err)
	}
	for i, g := range h.graphs {
		if _, err := g.sessions.FindByToken(t.Context(), secondToken); !isDistinctUnauthorized(err, biz.ErrLoginSessionKickedOut) {
			t.Fatalf("graph %d kicked-out token = %v, want ErrLoginSessionKickedOut", i, err)
		}
	}
	h.requireSessionsOnEveryGraph(t, uid, 0)

	rdb := h.stores.adminRedis(t)
	if exists, err := rdb.Exists(t.Context(), h.accountSessionKey(uid)).Result(); err != nil || exists != 0 {
		t.Fatalf("Account Session after last revocation: exists=%d err=%v, want deleted", exists, err)
	}
}

// TestSixthLoginAcrossInstancesEvictsOldestSession proves the five-Session
// limit with logins spread across two instances and one device: the oldest
// Session is displaced with its distinct reason, and both instances agree.
func TestSixthLoginAcrossInstancesEvictsOldestSessionIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	uid := h.seed.user(t, h.graphs[0], 0)

	const logins = 6
	sessions := make([]*biz.LoginSession, logins)
	tokens := make([]string, logins)
	for i := range logins {
		sessions[i], tokens[i] = loginOnGraph(t, h.graphs[i%len(h.graphs)], uid, "desktop")
	}

	listed := h.requireSessionsOnEveryGraph(t, uid, 5)
	listedIDs := make(map[string]struct{}, len(listed))
	for _, session := range listed {
		listedIDs[session.ID] = struct{}{}
	}
	if _, evicted := listedIDs[sessions[0].ID]; evicted {
		t.Fatal("oldest Session survived the five-Session limit")
	}
	for _, session := range sessions[1:] {
		if _, kept := listedIDs[session.ID]; !kept {
			t.Fatalf("Session %q was displaced instead of the oldest", session.ID)
		}
	}

	for i, g := range h.graphs {
		if _, err := g.sessions.FindByToken(t.Context(), tokens[0]); !isDistinctUnauthorized(err, biz.ErrLoginSessionReplaced) {
			t.Fatalf("graph %d evicted token = %v, want ErrLoginSessionReplaced", i, err)
		}
		for _, token := range tokens[1:] {
			if _, err := g.sessions.FindByToken(t.Context(), token); err != nil {
				t.Fatalf("graph %d rejected surviving token: %v", i, err)
			}
		}
	}
}

// authDenialErrors is the full distinct-401 taxonomy; tests use it to prove a
// failure carries exactly one reason and never collapses two into one.
var authDenialErrors = []error{
	biz.ErrTokenInvalid,
	biz.ErrTokenExpired,
	biz.ErrTokenIdleTimeout,
	biz.ErrLoginSessionKickedOut,
	biz.ErrLoginSessionReplaced,
}

// isDistinctUnauthorized reports err as exactly the wanted 401 reason — never
// the backend-unavailable 503 and never one of the other 401 reasons.
func isDistinctUnauthorized(err, want error) bool {
	if !errors.Is(err, want) || errors.Is(err, biz.ErrAuthBackendUnavailable) {
		return false
	}
	for _, other := range authDenialErrors {
		if other != want && errors.Is(err, other) {
			return false
		}
	}
	return true
}
