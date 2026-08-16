package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"glex/internal/biz"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core"
	satokensession "github.com/sa-tokens/sa-token-go/core/session"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

func newTestLoginSessionRepo(t *testing.T) (biz.LoginSessionRepo, *core.Manager, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	storage := newAuthStorage(rdb, "glex:auth", time.Second, time.Second)
	manager, cleanup, err := NewAuthManager(storage, testAuthConf())
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanup)
	data := &Data{
		rdb:               rdb,
		redisReadTimeout:  time.Second,
		redisWriteTimeout: time.Second,
	}
	return NewLoginSessionRepo(data, manager, testAuthConf()), manager, rdb
}

func createManagedLoginSession(t *testing.T, repo biz.LoginSessionRepo, manager *core.Manager, userID int64, userName, device string, loginTime time.Time) (*biz.LoginSession, string) {
	t.Helper()
	token := issueTestBearer(t, manager, userID, userName, device)
	ipAddress := "192.0.2.1"
	if userName == "bob" {
		ipAddress = "192.0.2.2"
	}
	session, err := repo.Create(t.Context(), token, &biz.LoginSession{
		SystemUserID: userID,
		UserName:     userName,
		DeviceType:   device,
		IPAddress:    ipAddress,
		Browser:      "Firefox",
		OS:           "Linux",
		LoginTime:    loginTime,
	})
	if err != nil {
		t.Fatalf("Create Login Session: %v", err)
	}
	return session, token
}

func TestLoginSessionsWithSameDeviceRemainEnumerable(t *testing.T) {
	repo, manager, _ := newTestLoginSessionRepo(t)
	first, firstToken := createManagedLoginSession(t, repo, manager, 42, "alice", "desktop", time.Unix(1_700_000_001, 0))
	second, secondToken := createManagedLoginSession(t, repo, manager, 42, "alice", "desktop", time.Unix(1_700_000_002, 0))

	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		t.Fatalf("Session IDs must be distinct and non-empty: %q, %q", first.ID, second.ID)
	}
	if first.ID == firstToken || second.ID == secondToken {
		t.Fatal("Session ID must be separate from the bearer token")
	}

	got, err := repo.ListBySystemUser(t.Context(), 42)
	if err != nil {
		t.Fatalf("ListBySystemUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListBySystemUser returned %d Sessions, want 2", len(got))
	}
	if got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("Sessions = [%q, %q], want registration order [%q, %q]", got[0].ID, got[1].ID, first.ID, second.ID)
	}
}

func TestCreateLoginSessionRejectsUnmanagedBearerToken(t *testing.T) {
	repo, _, _ := newTestLoginSessionRepo(t)
	_, err := repo.Create(t.Context(), "not-issued-by-manager", &biz.LoginSession{
		SystemUserID: 42,
		UserName:     "alice",
		DeviceType:   "desktop",
		LoginTime:    time.Unix(1_700_000_100, 0),
	})
	if !errors.Is(err, biz.ErrTokenInvalid) {
		t.Fatalf("Create(unmanaged token) = %v, want ErrTokenInvalid", err)
	}
	listed, err := repo.ListBySystemUser(t.Context(), 42)
	if err != nil {
		t.Fatalf("ListBySystemUser: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("unmanaged token created %d Login Sessions, want 0", len(listed))
	}
}

func TestConcurrentRegistrationKeepsFiveUsableSessionsAndReplacesOldest(t *testing.T) {
	repo, manager, _ := newTestLoginSessionRepo(t)
	const registrations = 6
	tokens := make([]string, registrations)
	for i := range tokens {
		tokens[i] = issueTestBearer(t, manager, 42, "alice", "desktop")
	}

	type created struct {
		session *biz.LoginSession
		token   string
		err     error
	}
	results := make(chan created, registrations)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(registrations)
	for i, token := range tokens {
		go func(i int, token string) {
			ready.Done()
			<-start
			session, err := repo.Create(t.Context(), token, &biz.LoginSession{
				SystemUserID: 42,
				UserName:     "alice",
				DeviceType:   "desktop",
				LoginTime:    time.Unix(1_700_001_000+int64(i), 0),
			})
			results <- created{session: session, token: token, err: err}
		}(i, token)
	}
	ready.Wait()
	close(start)

	byToken := make(map[string]*biz.LoginSession, registrations)
	for range registrations {
		result := <-results
		if result.err != nil {
			t.Fatalf("Create: %v", result.err)
		}
		byToken[result.token] = result.session
	}

	listed, err := repo.ListBySystemUser(t.Context(), 42)
	if err != nil {
		t.Fatalf("ListBySystemUser: %v", err)
	}
	if len(listed) != 5 {
		t.Fatalf("listed %d Sessions, want 5", len(listed))
	}
	listedIDs := make(map[string]struct{}, len(listed))
	for _, session := range listed {
		listedIDs[session.ID] = struct{}{}
	}

	valid, replaced := 0, 0
	for token, session := range byToken {
		_, err := manager.CheckLoginWithState(token)
		switch ClassifyAuthFailure(err) {
		case AuthFailureNone:
			valid++
			if _, ok := listedIDs[session.ID]; !ok {
				t.Errorf("usable Session %q is missing from the authoritative list", session.ID)
			}
		case AuthFailureReplaced:
			replaced++
			if _, ok := listedIDs[session.ID]; ok {
				t.Errorf("replaced Session %q remains listed", session.ID)
			}
		default:
			t.Errorf("token %s has unexpected state: %v", fmt.Sprint(session.ID), err)
		}
	}
	if valid != 5 || replaced != 1 {
		t.Fatalf("states = %d valid/%d replaced, want 5/1", valid, replaced)
	}
}

func TestConcurrentRegistrationAgainstRealRedis(t *testing.T) {
	addr := os.Getenv("GLEX_REAL_REDIS_ADDR")
	if addr == "" {
		t.Skip("set GLEX_REAL_REDIS_ADDR to run the real-Redis concurrency contract")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(t.Context()).Err(); err != nil {
		t.Fatalf("Ping real Redis: %v", err)
	}
	c := testAuthConf()
	c.RedisNamespace = "glex:test:login-session:" + uuid.NewString()
	storage := newAuthStorage(rdb, c.RedisNamespace, time.Second, time.Second)
	manager, cleanup, err := NewAuthManager(storage, c)
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	t.Cleanup(cleanup)
	data := &Data{
		rdb:               rdb,
		redisReadTimeout:  time.Second,
		redisWriteTimeout: time.Second,
	}
	repo := NewLoginSessionRepo(data, manager, c)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		keys, err := rdb.Keys(ctx, normalizeNamespace(c.RedisNamespace)+"*").Result()
		if err == nil && len(keys) > 0 {
			_ = rdb.Unlink(ctx, keys...).Err()
		}
	})

	const registrations = 6
	tokens := make([]string, registrations)
	for i := range tokens {
		tokens[i] = issueTestBearer(t, manager, 42, "alice", "desktop")
	}
	type result struct {
		token string
		err   error
	}
	results := make(chan result, registrations)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(registrations)
	for i, token := range tokens {
		go func(i int, token string) {
			ready.Done()
			<-start
			_, err := repo.Create(t.Context(), token, &biz.LoginSession{
				SystemUserID: 42,
				UserName:     "alice",
				DeviceType:   "desktop",
				LoginTime:    time.Unix(1_700_001_100+int64(i), 0),
			})
			results <- result{token: token, err: err}
		}(i, token)
	}
	ready.Wait()
	close(start)
	for range registrations {
		result := <-results
		if result.err != nil {
			t.Fatalf("Create against real Redis: %v", result.err)
		}
	}
	replaced := 0
	for _, token := range tokens {
		_, err := manager.CheckLoginWithState(token)
		if ClassifyAuthFailure(err) == AuthFailureReplaced {
			replaced++
		} else if err != nil {
			t.Fatalf("unexpected token state: %v", err)
		}
	}
	listed, err := repo.ListBySystemUser(t.Context(), 42)
	if err != nil {
		t.Fatalf("ListBySystemUser: %v", err)
	}
	if len(listed) != 5 || replaced != 1 {
		t.Fatalf("real Redis result = %d listed/%d replaced, want 5/1", len(listed), replaced)
	}
}

func TestLogoutOldSessionDoesNotHideNewerTokenAndLastLogoutDeletesAccountSession(t *testing.T) {
	repo, manager, rdb := newTestLoginSessionRepo(t)
	first, firstToken := createManagedLoginSession(t, repo, manager, 42, "alice", "desktop", time.Unix(1_700_002_001, 0))
	second, secondToken := createManagedLoginSession(t, repo, manager, 42, "alice", "desktop", time.Unix(1_700_002_002, 0))

	if err := repo.Logout(t.Context(), first.ID); err != nil {
		t.Fatalf("Logout(old): %v", err)
	}
	if _, err := manager.CheckLoginWithState(firstToken); !errors.Is(err, ErrAuthKeyNotFound) {
		t.Fatalf("old token state = %v, want removed", err)
	}
	if _, err := manager.CheckLoginWithState(secondToken); err != nil {
		t.Fatalf("newer same-device token was orphaned: %v", err)
	}
	lookedUp, err := repo.FindByToken(t.Context(), secondToken)
	if err != nil {
		t.Fatalf("FindByToken(newer): %v", err)
	}
	if lookedUp.ID != second.ID {
		t.Fatalf("FindByToken ID = %q, want %q", lookedUp.ID, second.ID)
	}
	if exists, err := rdb.Exists(t.Context(), "glex:auth:"+authManagerKeyPrefix+satokensession.AccountSessionKeyPrefix+"42").Result(); err != nil || exists != 1 {
		t.Fatalf("Account Session after first Logout: exists=%d err=%v, want present", exists, err)
	}

	if err := repo.Logout(t.Context(), second.ID); err != nil {
		t.Fatalf("Logout(last): %v", err)
	}
	if exists, err := rdb.Exists(t.Context(), "glex:auth:"+authManagerKeyPrefix+satokensession.AccountSessionKeyPrefix+"42").Result(); err != nil || exists != 0 {
		t.Fatalf("Account Session after last Logout: exists=%d err=%v, want deleted", exists, err)
	}
	listed, err := repo.ListBySystemUser(t.Context(), 42)
	if err != nil {
		t.Fatalf("ListBySystemUser after Logout: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed %d Sessions after last Logout, want 0", len(listed))
	}
}

func TestOwnershipCheckedLogoutCannotRevokeAnotherSystemUsersSession(t *testing.T) {
	repo, manager, _ := newTestLoginSessionRepo(t)
	other, otherToken := createManagedLoginSession(t, repo, manager, 84, "bob", "mobile", time.Unix(1_700_003_001, 0))

	err := repo.LogoutOwned(t.Context(), 42, other.ID)
	if !errors.Is(err, biz.ErrLoginSessionNotOwned) {
		t.Fatalf("LogoutOwned(other user) = %v, want ErrLoginSessionNotOwned", err)
	}
	if _, err := manager.CheckLoginWithState(otherToken); err != nil {
		t.Fatalf("other user's token was revoked: %v", err)
	}
	owned, err := repo.Owns(t.Context(), 84, other.ID)
	if err != nil {
		t.Fatalf("Owns: %v", err)
	}
	if !owned {
		t.Fatal("Session owner was not recognized")
	}
}

func TestSnowflakeSystemUserIDKeepsOwnershipAndLastSessionCleanupExact(t *testing.T) {
	repo, manager, rdb := newTestLoginSessionRepo(t)
	const systemUserID int64 = 9_007_199_254_740_993 // first integer above IEEE-754's exact range
	session, _ := createManagedLoginSession(t, repo, manager, systemUserID, "alice", "desktop", time.Unix(1_700_003_100, 0))

	if err := repo.LogoutOwned(t.Context(), systemUserID, session.ID); err != nil {
		t.Fatalf("LogoutOwned(Snowflake ID): %v", err)
	}
	accountKey := "glex:auth:" + authManagerKeyPrefix + satokensession.AccountSessionKeyPrefix + strconv.FormatInt(systemUserID, 10)
	if exists, err := rdb.Exists(t.Context(), accountKey).Result(); err != nil || exists != 0 {
		t.Fatalf("Account Session after Snowflake owner's last Logout: exists=%d err=%v, want deleted", exists, err)
	}
}

func TestTargetedRevocationPreservesKickoutAndReplacementReasons(t *testing.T) {
	repo, manager, _ := newTestLoginSessionRepo(t)
	kicked, kickedToken := createManagedLoginSession(t, repo, manager, 42, "alice", "desktop", time.Unix(1_700_004_001, 0))
	replaced, replacedToken := createManagedLoginSession(t, repo, manager, 42, "alice", "mobile", time.Unix(1_700_004_002, 0))

	if err := repo.Kickout(t.Context(), kicked.ID); err != nil {
		t.Fatalf("Kickout: %v", err)
	}
	if _, err := repo.FindByToken(t.Context(), kickedToken); !errors.Is(err, biz.ErrLoginSessionKickedOut) {
		t.Fatalf("FindByToken(kicked) = %v, want ErrLoginSessionKickedOut", err)
	}
	if err := repo.Replace(t.Context(), replaced.ID); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, err := repo.FindByToken(t.Context(), replacedToken); !errors.Is(err, biz.ErrLoginSessionReplaced) {
		t.Fatalf("FindByToken(replaced) = %v, want ErrLoginSessionReplaced", err)
	}
	if err := repo.Kickout(t.Context(), kicked.ID); err != nil {
		t.Fatalf("idempotent Kickout: %v", err)
	}
}

func TestRevokeSystemUserSessionsCanPreserveCurrentSession(t *testing.T) {
	repo, manager, rdb := newTestLoginSessionRepo(t)
	first, firstToken := createManagedLoginSession(t, repo, manager, 42, "alice", "desktop", time.Unix(1_700_005_001, 0))
	current, currentToken := createManagedLoginSession(t, repo, manager, 42, "alice", "mobile", time.Unix(1_700_005_002, 0))
	other, otherToken := createManagedLoginSession(t, repo, manager, 84, "bob", "desktop", time.Unix(1_700_005_003, 0))

	if err := repo.RevokeSystemUser(t.Context(), 42, current.ID); err != nil {
		t.Fatalf("RevokeSystemUser(except current): %v", err)
	}
	if _, err := repo.FindByToken(t.Context(), firstToken); !errors.Is(err, biz.ErrLoginSessionKickedOut) {
		t.Fatalf("first Session state = %v, want kicked out", err)
	}
	if _, err := repo.FindByToken(t.Context(), currentToken); err != nil {
		t.Fatalf("preserved current Session: %v", err)
	}
	if _, err := repo.FindByToken(t.Context(), otherToken); err != nil {
		t.Fatalf("other System User was affected: %v", err)
	}

	all, err := repo.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 || all[0].ID != current.ID || all[1].ID != other.ID {
		t.Fatalf("global Sessions = %#v, want current and other in registration order", all)
	}
	if first.ID == current.ID {
		t.Fatal("test setup produced duplicate Session IDs")
	}

	if err := repo.RevokeSystemUser(t.Context(), 42, ""); err != nil {
		t.Fatalf("RevokeSystemUser(all): %v", err)
	}
	if _, err := repo.FindByToken(t.Context(), currentToken); !errors.Is(err, biz.ErrLoginSessionKickedOut) {
		t.Fatalf("current Session state after full revoke = %v, want kicked out", err)
	}
	accountKey := "glex:auth:" + authManagerKeyPrefix + satokensession.AccountSessionKeyPrefix + "42"
	if exists, err := rdb.Exists(t.Context(), accountKey).Result(); err != nil || exists != 0 {
		t.Fatalf("Account Session after full revoke: exists=%d err=%v, want deleted", exists, err)
	}
}

func TestRevokeSystemUsersIsOneAtomicValidatedBatch(t *testing.T) {
	repo, manager, rdb := newTestLoginSessionRepo(t)
	first, firstToken := createManagedLoginSession(t, repo, manager, 42, "alice", "desktop", time.Unix(1_700_005_101, 0))
	second, _ := createManagedLoginSession(t, repo, manager, 84, "bob", "mobile", time.Unix(1_700_005_102, 0))

	// A malformed later user's metadata makes the Lua validation phase fail.
	// Because validation precedes every write, the earlier user remains live.
	if err := rdb.Set(t.Context(), "glex:auth:"+loginSessionMetadataPrefix+second.ID, "{", time.Hour).Err(); err != nil {
		t.Fatalf("corrupt later metadata: %v", err)
	}
	err := repo.RevokeSystemUsers(t.Context(), []biz.SystemUserSessionRevocation{
		{SystemUserID: 42, Policy: biz.RevokeAllLoginSessions()},
		{SystemUserID: 84, Policy: biz.RevokeAllLoginSessions()},
	})
	if err == nil {
		t.Fatal("atomic batch accepted malformed later metadata")
	}
	if got, err := repo.FindByID(t.Context(), first.ID); err != nil || got.ID != first.ID {
		t.Fatalf("earlier user was partially revoked: %#v/%v", got, err)
	}
	if _, err := repo.FindByToken(t.Context(), firstToken); err != nil {
		t.Fatalf("earlier bearer was partially revoked: %v", err)
	}
}

func TestListLoginSessionsFiltersAndOrdersBeforePagination(t *testing.T) {
	repo, manager, _ := newTestLoginSessionRepo(t)
	older, _ := createManagedLoginSession(t, repo, manager, 42, "alice", "desktop", time.Unix(1_700_006_001, 0))
	newer, _ := createManagedLoginSession(t, repo, manager, 42, "alice", "mobile", time.Unix(1_700_006_003, 0))
	createManagedLoginSession(t, repo, manager, 84, "bob", "desktop", time.Unix(1_700_006_002, 0))

	declarations, err := filtering.NewDeclarations(
		filtering.DeclareStandardFunctions(),
		filtering.DeclareIdent("user_name", filtering.TypeString),
		filtering.DeclareIdent("ipaddr", filtering.TypeString),
	)
	if err != nil {
		t.Fatalf("NewDeclarations: %v", err)
	}
	filter, err := filtering.ParseFilterString(`user_name = "alice" AND ipaddr = "192.0.2.1"`, declarations)
	if err != nil {
		t.Fatalf("ParseFilterString: %v", err)
	}
	got, err := repo.List(t.Context(),
		biz.ListFilter(filter),
		biz.ListOrderBy(ordering.OrderBy{Fields: []ordering.Field{{Path: "login_time", Desc: true}}}),
		biz.ListOffset(1),
		biz.ListLimit(1),
	)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != older.ID {
		t.Fatalf("filtered/ordered page = %#v, want older alice Session %q after skipping %q", got, older.ID, newer.ID)
	}
}
