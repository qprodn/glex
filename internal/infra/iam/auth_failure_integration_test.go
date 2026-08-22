package iaminfra

import (
	"glex/internal/infra/store"

	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	iamapp "glex/internal/application/iam"
	"glex/internal/conf"
	"glex/internal/domain/iam"

	"github.com/redis/go-redis/v9"
	satokenmanager "github.com/sa-tokens/sa-token-go/core/manager"
	"google.golang.org/protobuf/proto"
)

// These tests inject failures at the protocol points the error contract
// depends on — Redis reads (authentication), Redis writes (login and
// revocation), Redis timeouts, and malformed storage — and prove every one
// surfaces as the 503 AUTH_BACKEND_UNAVAILABLE reason, never as a 401/403
// authentication decision.

func requireServiceUnavailable(t *testing.T, operation string, err error) {
	t.Helper()
	if !errors.Is(err, iam.ErrAuthBackendUnavailable) {
		t.Fatalf("%s = %v, want ErrAuthBackendUnavailable (503)", operation, err)
	}
	denials := append(append([]error(nil), authDenialErrors...), iam.ErrPermissionDenied)
	for _, denied := range denials {
		if errors.Is(err, denied) {
			t.Fatalf("%s collapsed into an authentication denial: %v", operation, err)
		}
	}
}

// TestRedisReadWriteFailureReturnsServiceUnavailable closes one instance's
// real Redis client mid-flight: every protocol point — authenticate (read),
// list (read), logout and revocation (write) — must fail as 503 while the
// other instance, on its own connections, keeps serving the same stores.
func TestRedisReadWriteFailureReturnsServiceUnavailableIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	uid := h.seed.user(t, h.graphs[0], 0)
	session, token := loginOnGraph(t, h.graphs[0], uid, "desktop")

	if err := h.graphs[0].rdb.Close(); err != nil {
		t.Fatalf("close graph 0 Redis client: %v", err)
	}
	_, err := h.graphs[0].authorizer.Authenticate(t.Context(), token)
	requireServiceUnavailable(t, "Authenticate over closed Redis", err)

	_, err = h.graphs[0].sessions.ListBySystemUser(t.Context(), uid)
	requireServiceUnavailable(t, "ListBySystemUser over closed Redis", err)

	err = h.graphs[0].sessions.Logout(t.Context(), session.ID)
	requireServiceUnavailable(t, "Logout over closed Redis", err)

	err = h.graphs[0].sessions.Kickout(t.Context(), session.ID)
	requireServiceUnavailable(t, "Kickout over closed Redis", err)

	// The session state itself is intact — the failure was transport-only.
	if _, err := h.graphs[1].sessions.FindByToken(t.Context(), token); err != nil {
		t.Fatalf("healthy instance lost the Session during the other instance's outage: %v", err)
	}
}

// TestLoginWriteFailureRollsBackAndRegistersNothingIntegration fails Redis
// before a login: no credential is returned, PostgreSQL records no last
// login, and no token keys leak into the namespace.
func TestLoginWriteFailureRollsBackAndRegistersNothingIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	uid := h.seed.user(t, h.graphs[0], 0)
	if err := h.graphs[0].rdb.Close(); err != nil {
		t.Fatalf("close graph 0 Redis client: %v", err)
	}

	_, err := h.graphs[0].authUC.Login(t.Context(), &iamapp.LoginRequest{
		UserName:  integrationUserName(uid),
		Password:  integrationLoginPassword,
		SourceIP:  "192.0.2.1",
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) Firefox/1.0",
	})
	requireServiceUnavailable(t, "Login over closed Redis", err)

	listed, err := h.graphs[1].sessions.ListBySystemUser(t.Context(), uid)
	if err != nil || len(listed) != 0 {
		t.Fatalf("failed login registered %d Sessions (%v), want 0", len(listed), err)
	}
	user, err := h.graphs[1].db.User.Get(t.Context(), uid)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if user.LoginIP != "" || user.LoginDate != nil {
		t.Fatalf("failed login updated last-login state: ip=%q date=%v", user.LoginIP, user.LoginDate)
	}
	keys := h.stores.adminRedis(t).Keys(t.Context(), h.stores.rawKey(authManagerTokenKeyPattern())).Val()
	if len(keys) != 0 {
		t.Fatalf("failed login left %d token keys behind: %v", len(keys), keys)
	}
}

func authManagerTokenKeyPattern() string {
	return authManagerKeyPrefix + satokenmanager.TokenKeyPrefix + "*"
}

// TestRedisTimeoutFailsBoundedAndUnavailable runs the authentication stack
// against a server that accepts connections and never answers: the failure
// must be a 503, reached within the bounded operation timeout — not a hang
// and not a denial. This test needs no real stores.
func TestRedisTimeoutFailsBoundedAndUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Hold the connection open; never reply.
			_ = conn
		}
	}()

	const timeout = 250 * time.Millisecond
	rdb := redis.NewClient(&redis.Options{
		Addr:         listener.Addr().String(),
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		PoolTimeout:  2 * timeout,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	conf := proto.Clone(testAuthConf()).(*conf.Auth)
	conf.RedisNamespace = "glex:test:blackhole"
	data := &store.Data{Redis: rdb, Logger: testLogger(), RedisReadTimeout: timeout, RedisWriteTimeout: timeout}
	storage := newAuthStorage(rdb, conf.RedisNamespace, timeout, timeout, testLogger())
	manager, cleanupManager, err := NewAuthManager(storage, conf)
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanupManager)
	sessions := NewLoginSessionRepo(data, manager, conf)
	authenticator := NewAuthenticator(manager, sessions)

	start := time.Now()
	_, authErr := authenticator.Authenticate(t.Context(), "any-bearer")
	elapsed := time.Since(start)
	requireServiceUnavailable(t, "Authenticate against silent Redis", authErr)
	if elapsed > 2*time.Second {
		t.Fatalf("Authenticate against silent Redis took %v, want bounded below 2s", elapsed)
	}
}

// TestMalformedLoginSessionMetadataReturnsServiceUnavailable corrupts the
// authoritative metadata of one live Session: every reader must classify the
// damage as a 503 backend failure, never as an authentication denial or an
// empty answer.
func TestMalformedLoginSessionMetadataReturnsServiceUnavailableIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	uid := h.seed.user(t, h.graphs[0], 0)
	session, token := loginOnGraph(t, h.graphs[0], uid, "desktop")

	admin := h.stores.adminRedis(t)
	if err := admin.Set(t.Context(), h.stores.rawKey(loginSessionMetadataPrefix+session.ID), "{", time.Hour).Err(); err != nil {
		t.Fatalf("corrupt Login Session metadata: %v", err)
	}

	for i, g := range h.graphs {
		_, err := g.sessions.FindByToken(t.Context(), token)
		requireServiceUnavailable(t, graphName(i, "FindByToken over malformed metadata"), err)

		_, err = g.sessions.FindByID(t.Context(), session.ID)
		requireServiceUnavailable(t, graphName(i, "FindByID over malformed metadata"), err)

		_, err = g.sessions.ListBySystemUser(t.Context(), uid)
		requireServiceUnavailable(t, graphName(i, "ListBySystemUser over malformed metadata"), err)

		principal, err := g.authorizer.Authenticate(t.Context(), token)
		requireServiceUnavailable(t, graphName(i, "Authenticate over malformed metadata"), err)
		if principal != nil {
			t.Fatalf("graph %d returned a Principal from malformed metadata", i)
		}
	}
}

// TestCorruptAccountSessionFailsClosedIntegration destroys the shared
// Authorization Snapshot itself: authentication must fail closed as 503 with
// no Principal — never degrade to an empty-permission identity that would
// answer permission checks with a clean 403.
func TestCorruptAccountSessionFailsClosedIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	g := h.graphs[0]
	uid := h.seed.user(t, g, 0)
	role := h.seed.role(t, g, 0)
	menu := h.seed.menu(t, g, "one:perm", 0)
	if _, err := g.userRoles.ReplaceUserRoles(t.Context(), uid, []int64{role}); err != nil {
		t.Fatalf("seed Role Assignment: %v", err)
	}
	if _, err := g.roles.ReplaceRoleMenus(t.Context(), role, []int64{menu}, true); err != nil {
		t.Fatalf("seed Menu Grant: %v", err)
	}
	_, token := loginOnGraph(t, h.graphs[0], uid, "desktop")

	admin := h.stores.adminRedis(t)
	if err := admin.Set(t.Context(), h.accountSessionKey(uid), "not-session-json", time.Hour).Err(); err != nil {
		t.Fatalf("corrupt Account Session: %v", err)
	}

	for i, graph := range h.graphs {
		principal, err := graph.authorizer.Authenticate(t.Context(), token)
		requireServiceUnavailable(t, graphName(i, "Authenticate over corrupt Account Session"), err)
		if principal != nil {
			t.Fatalf("graph %d built Principal %v from a corrupt Account Session", i, principal)
		}
	}
}

// TestRevokeBatchValidatesAllBeforeWritingAgainstRealRedis repeats the Lua
// atomicity contract against a real EVAL: malformed metadata for a later
// System User aborts the whole revocation batch and leaves the earlier user
// untouched.
func TestRevokeBatchValidatesAllBeforeWritingAgainstRealRedisIntegration(t *testing.T) {
	h := newCrossInstanceIntegration(t)
	first := h.seed.user(t, h.graphs[0], 0)
	second := h.seed.user(t, h.graphs[0], 0)
	firstSession, firstToken := loginOnGraph(t, h.graphs[0], first, "desktop")
	secondSession, _ := loginOnGraph(t, h.graphs[1], second, "mobile")

	admin := h.stores.adminRedis(t)
	if err := admin.Set(t.Context(), h.stores.rawKey(loginSessionMetadataPrefix+secondSession.ID), "{", time.Hour).Err(); err != nil {
		t.Fatalf("corrupt later metadata: %v", err)
	}

	err := h.graphs[0].sessions.RevokeSystemUsers(t.Context(), []iam.SystemUserSessionRevocation{
		{SystemUserID: first, Policy: iam.RevokeAllLoginSessions()},
		{SystemUserID: second, Policy: iam.RevokeAllLoginSessions()},
	})
	requireServiceUnavailable(t, "RevokeSystemUsers over malformed later metadata", err)

	if _, err := h.graphs[1].sessions.FindByToken(t.Context(), firstToken); err != nil {
		t.Fatalf("earlier user was partially revoked: %v", err)
	}
	if _, err := h.graphs[1].sessions.FindByID(t.Context(), firstSession.ID); err != nil {
		t.Fatalf("earlier Session was partially revoked: %v", err)
	}
}

// TestIntegrationFlowsNeverLogBearerTokens drives a mixed success/failure
// flow — logins, authentication, kickout, an injected outage with its warning
// logs, and malformed storage — with the process logger captured. Raw bearer
// credentials must never reach log output.
func TestIntegrationFlowsNeverLogBearerTokensIntegration(t *testing.T) {
	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	h := newCrossInstanceIntegrationWithLogger(t, logger)
	uid := h.seed.user(t, h.graphs[0], 0)
	first, firstToken := loginOnGraph(t, h.graphs[0], uid, "desktop")
	_, secondToken := loginOnGraph(t, h.graphs[1], uid, "mobile")
	for _, g := range h.graphs {
		for _, token := range []string{firstToken, secondToken} {
			_, _ = g.authorizer.Authenticate(t.Context(), token)
		}
	}
	if err := h.graphs[0].sessions.Kickout(t.Context(), first.ID); err != nil {
		t.Fatalf("Kickout: %v", err)
	}
	if err := h.stores.adminRedis(t).Set(t.Context(),
		h.stores.rawKey(loginSessionMetadataPrefix+first.ID), "{", time.Hour).Err(); err != nil {
		t.Fatalf("corrupt metadata: %v", err)
	}
	_, _ = h.graphs[1].authorizer.Authenticate(t.Context(), firstToken)

	// The outage path is where warnings (op + key class + driver error) get
	// emitted; it must still not carry the credential.
	if err := h.graphs[0].rdb.Close(); err != nil {
		t.Fatalf("close graph 0 Redis client: %v", err)
	}
	_, _ = h.graphs[0].authorizer.Authenticate(t.Context(), secondToken)
	_, _ = h.graphs[0].sessions.ListBySystemUser(t.Context(), uid)
	_ = h.graphs[0].sessions.Logout(t.Context(), first.ID)

	logs := buf.String()
	// The no-leak assertion is only meaningful if the failure path actually
	// emitted its warnings; require them rather than pass vacuously.
	if !strings.Contains(logs, "operation failed") {
		t.Fatalf("injected failures emitted no warning logs; log capture is broken: %q", logs)
	}
	for _, token := range []string{firstToken, secondToken} {
		if strings.Contains(logs, token) {
			t.Fatalf("raw bearer token leaked into logs: %s", logs)
		}
	}
}

func graphName(index int, operation string) string {
	return "graph " + strconv.Itoa(index) + " " + operation
}
