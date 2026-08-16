package data

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"glex/internal/conf"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core/errs"
	"google.golang.org/protobuf/types/known/durationpb"
)

func testAuthConf() *conf.Auth {
	return &conf.Auth{
		FixedLifetime:                  durationpb.New(7 * 24 * time.Hour),
		IdleLifetime:                   durationpb.New(30 * time.Minute),
		MaxSessionsPerUser:             5,
		RedisNamespace:                 "glex:auth",
		EdgeProxyAuthoritativeSourceIp: true,
		UsernameLoginCounter: &conf.Auth_LoginCounter{
			MaxFailures: 5,
			Window:      durationpb.New(10 * time.Minute),
		},
		SourceIpLoginCounter: &conf.Auth_LoginCounter{
			MaxFailures: 20,
			Window:      durationpb.New(10 * time.Minute),
		},
	}
}

func TestValidateAuthConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*conf.Auth)
	}{
		{"zero fixed lifetime", func(c *conf.Auth) { c.FixedLifetime = durationpb.New(0) }},
		{"zero idle lifetime", func(c *conf.Auth) { c.IdleLifetime = durationpb.New(0) }},
		{"idle not shorter than fixed", func(c *conf.Auth) {
			c.IdleLifetime = durationpb.New(8 * 24 * time.Hour)
		}},
		{"zero max sessions", func(c *conf.Auth) { c.MaxSessionsPerUser = 0 }},
		{"empty namespace", func(c *conf.Auth) { c.RedisNamespace = "  " }},
		{"untrusted source IP topology", func(c *conf.Auth) { c.EdgeProxyAuthoritativeSourceIp = false }},
		{"missing username counter", func(c *conf.Auth) { c.UsernameLoginCounter = nil }},
		{"zero counter failures", func(c *conf.Auth) { c.UsernameLoginCounter.MaxFailures = 0 }},
		{"zero counter window", func(c *conf.Auth) { c.SourceIpLoginCounter.Window = durationpb.New(0) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testAuthConf()
			tc.mutate(c)
			if err := validateAuthConfig(c); err == nil {
				t.Fatalf("validateAuthConfig(%s) = nil, want error", tc.name)
			}
		})
	}
	t.Run("nil config", func(t *testing.T) {
		if err := validateAuthConfig(nil); err == nil {
			t.Fatal("validateAuthConfig(nil) = nil, want error")
		}
	})
}

func TestValidateAuthConfigAcceptsConfiguredDefaults(t *testing.T) {
	if err := validateAuthConfig(testAuthConf()); err != nil {
		t.Fatalf("validateAuthConfig(defaults) = %v, want nil", err)
	}
}

func TestNewAuthManager(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	storage := newAuthStorage(rdb, "glex:auth", time.Second, time.Second)

	manager, cleanup, err := NewAuthManager(storage, testAuthConf())
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	if manager == nil {
		t.Fatal("manager is nil")
	}
	cleanup()

	if _, _, err := NewAuthManager(storage, nil); err == nil {
		t.Fatal("NewAuthManager(nil config) must fail")
	}
}

// TestNewAuthManagerProducesDistinctTokens proves the Manager configuration
// really yields opaque per-login tokens under concurrent login with no
// sharing — the contract tickets 03/04 build on.
func TestNewAuthManagerProducesDistinctTokens(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	storage := newAuthStorage(rdb, "glex:auth", time.Second, time.Second)

	manager, cleanup, err := NewAuthManager(storage, testAuthConf())
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	defer cleanup()

	first := issueTestBearer(t, manager, 42, "alice", "web")
	second := issueTestBearer(t, manager, 42, "alice", "web")
	if first == "" || second == "" || first == second {
		t.Fatalf("logins must issue distinct non-empty tokens, got %q and %q", first, second)
	}

	// Both tokens stay valid: the Manager imposes no device-mapping limit
	// (MaxLoginCount=-1); the project index owns eviction.
	for i, token := range []string{first, second} {
		if err := manager.CheckLogin(token); err != nil {
			t.Fatalf("CheckLogin #%d: %v", i+1, err)
		}
	}
}

func TestClassifyAuthFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want AuthFailureKind
	}{
		{"nil", nil, AuthFailureNone},
		{"not login", errs.ErrNotLogin, AuthFailureNotLogin},
		{"wrapped not login", fmt.Errorf("wrap: %w", errs.ErrNotLogin), AuthFailureNotLogin},
		{"invalid", errs.ErrTokenInvalid, AuthFailureInvalidToken},
		{"expired", errs.ErrTokenExpired, AuthFailureExpired},
		{"idle timeout", errs.ErrActiveTimeoutWithToken("tok"), AuthFailureIdleTimeout},
		{"kickout", errs.ErrKickedOutWithToken("tok"), AuthFailureKickout},
		{"replaced", errs.ErrTokenReplacedWithToken("tok"), AuthFailureReplaced},
		{"max login count", errs.ErrMaxLoginCount, AuthFailureReplaced},
		{"adapter backend failure", fmt.Errorf("outer: %w", ErrAuthStorage), AuthFailureStorage},
		{"upstream storage failure", errs.ErrStorageWrap(errors.New("boom")), AuthFailureStorage},
		{"malformed session data", errs.ErrInvalidSessionData, AuthFailureStorage},
		{"unknown error", errors.New("something else"), AuthFailureStorage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyAuthFailure(tc.err); got != tc.want {
				t.Fatalf("ClassifyAuthFailure(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestAuthManagerStaysInNamespace is a cross-check that Manager-issued keys
// never escape the glex auth namespace in Redis.
func TestAuthManagerStaysInNamespace(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	storage := newAuthStorage(rdb, "glex:auth", time.Second, time.Second)

	manager, cleanup, err := NewAuthManager(storage, testAuthConf())
	if err != nil {
		t.Fatalf("NewAuthManager: %v", err)
	}
	defer cleanup()

	_ = issueTestBearer(t, manager, 42, "alice", "web")
	var leaked []string
	for _, key := range mr.Keys() {
		if !strings.HasPrefix(key, "glex:auth:") {
			leaked = append(leaked, key)
		}
	}
	if len(leaked) > 0 {
		t.Fatalf("sa-token wrote keys outside the glex:auth namespace: %v", leaked)
	}
}
