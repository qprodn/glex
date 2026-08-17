package data

import (
	"errors"
	"fmt"
	"sync"

	"glex/internal/conf"

	"github.com/sa-tokens/sa-token-go/core"
	"github.com/sa-tokens/sa-token-go/core/adapter"
	"github.com/sa-tokens/sa-token-go/core/errs"
	"google.golang.org/protobuf/types/known/durationpb"
)

// authManagerKeyPrefix is explicit because the Login Session Lua scripts must
// atomically update the same sa-token keys as Manager. Upstream otherwise
// supplies this value implicitly when Config.KeyPrefix is empty.
const authManagerKeyPrefix = "satoken"

// NewAuthManager constructs the sa-token Manager after explicitly validating
// the auth configuration. The Manager stays inside data; every caller above
// goes through repo interfaces (login, session store, middleware queries).
func NewAuthManager(storage adapter.Storage, c *conf.Auth) (*core.Manager, func(), error) {
	if err := validateAuthConfig(c); err != nil {
		return nil, nil, fmt.Errorf("invalid auth config: %w", err)
	}
	cfg := core.DefaultConfig()
	// Opaque random bearer tokens (ADR-0007).
	cfg.TokenStyle = core.TokenStyleSimple
	// Concurrent Login Sessions, each with its own token.
	cfg.IsConcurrent = true
	cfg.IsShare = false
	// The project-owned session index enforces the per-user limit (ticket 03);
	// sa-token's MaxLoginCount is deliberately unlimited because v0.2.x can
	// overwrite a device mapping while leaving the older token valid.
	cfg.MaxLoginCount = -1
	// Fixed seven-day lifetime: the token never renews on activity. The idle
	// lifetime is enforced by the explicit active-timeout check in middleware.
	cfg.Timeout = int64(c.GetFixedLifetime().AsDuration().Seconds())
	cfg.ActiveTimeout = int64(c.GetIdleLifetime().AsDuration().Seconds())
	cfg.AutoRenew = false
	cfg.MaxRefresh = -1
	// The storage adapter owns the glex auth namespace. Keep the upstream
	// manager prefix explicit so project Lua scripts share its key contract.
	cfg.KeyPrefix = authManagerKeyPrefix
	// Credential transport is project-owned (strict Bearer header only); the
	// Manager never reads tokens from cookies, body, or its own header logic.
	cfg.IsReadHeader = true
	cfg.IsReadCookie = false
	cfg.IsReadBody = false
	cfg.TokenPrefix = ""
	cfg.IsLog = false
	cfg.IsPrintBanner = false
	manager := core.NewManager(storage, cfg)
	var closeOnce sync.Once
	cleanup := func() { closeOnce.Do(manager.CloseManager) }
	return manager, cleanup, nil
}

// validateAuthConfig rejects incomplete or malformed auth settings before the
// Manager exists, so the process fails at startup instead of at login time.
func validateAuthConfig(c *conf.Auth) error {
	if c == nil {
		return errors.New("auth config is required")
	}
	if err := validatePositiveDuration("fixed_lifetime", c.GetFixedLifetime()); err != nil {
		return err
	}
	if err := validatePositiveDuration("idle_lifetime", c.GetIdleLifetime()); err != nil {
		return err
	}
	if c.GetIdleLifetime().AsDuration() >= c.GetFixedLifetime().AsDuration() {
		return errors.New("idle_lifetime must be shorter than fixed_lifetime")
	}
	if c.GetMaxSessionsPerUser() == 0 {
		return errors.New("max_sessions_per_user must be positive")
	}
	if ns := normalizeNamespace(c.GetRedisNamespace()); ns == "" || ns == ":" {
		return errors.New("redis_namespace must be a non-empty glex auth namespace")
	}
	if !c.GetEdgeProxyAuthoritativeSourceIp() {
		return errors.New("edge_proxy_authoritative_source_ip must confirm the exclusive edge-proxy deployment topology")
	}
	if err := validateLoginCounter("username_login_counter", c.GetUsernameLoginCounter()); err != nil {
		return err
	}
	if err := validateLoginCounter("source_ip_login_counter", c.GetSourceIpLoginCounter()); err != nil {
		return err
	}
	return nil
}

func validateLoginCounter(name string, c *conf.Auth_LoginCounter) error {
	if c == nil {
		return fmt.Errorf("%s is required", name)
	}
	if c.GetMaxFailures() == 0 {
		return fmt.Errorf("%s.max_failures must be positive", name)
	}
	return validatePositiveDuration(name+".window", c.GetWindow())
}

func validatePositiveDuration(name string, duration *durationpb.Duration) error {
	if duration == nil {
		return fmt.Errorf("%s is required", name)
	}
	if err := duration.CheckValid(); err != nil {
		return fmt.Errorf("%s is malformed: %w", name, err)
	}
	if duration.AsDuration() <= 0 {
		return fmt.Errorf("%s must be positive", name)
	}
	return nil
}

// AuthFailureKind classifies why an authentication-state operation failed.
// Middleware maps each kind to a distinct 401 reason (or 503 for storage),
// so the classification must never collapse them into one "not logged in".
type AuthFailureKind int

const (
	// AuthFailureNone means the error is nil.
	AuthFailureNone AuthFailureKind = iota
	// AuthFailureNotLogin: no credential was presented / no login under this
	// token.
	AuthFailureNotLogin
	// AuthFailureInvalidToken: the token is malformed or corrupted.
	AuthFailureInvalidToken
	// AuthFailureExpired: the token passed its fixed lifetime.
	AuthFailureExpired
	// AuthFailureIdleTimeout: the token exceeded its active (idle) timeout.
	AuthFailureIdleTimeout
	// AuthFailureKickout: an administrator forcibly revoked the session.
	AuthFailureKickout
	// AuthFailureReplaced: the session was evicted by the session limit.
	AuthFailureReplaced
	// AuthFailureStorage: the auth backend failed — 503, never 401/403.
	// Unknown errors land here too: failing closed as a backend problem is
	// safe; mistaking a backend problem for "invalid token" is not.
	AuthFailureStorage
)

// ClassifyAuthFailure maps a sa-token error (possibly wrapped) onto its
// failure kind. It reads only sentinel matches, so wrapped causes from the
// Manager, the session package, or this adapter all classify the same.
func ClassifyAuthFailure(err error) AuthFailureKind {
	switch {
	case err == nil:
		return AuthFailureNone
	case errors.Is(err, errs.ErrNotLogin):
		return AuthFailureNotLogin
	case errors.Is(err, errs.ErrTokenInvalid):
		return AuthFailureInvalidToken
	case errors.Is(err, errs.ErrTokenExpired):
		return AuthFailureExpired
	case errors.Is(err, errs.ErrActiveTimeout):
		return AuthFailureIdleTimeout
	case errors.Is(err, errs.ErrKickedOut):
		return AuthFailureKickout
	case errors.Is(err, errs.ErrTokenReplaced):
		return AuthFailureReplaced
	case errors.Is(err, errs.ErrMaxLoginCount):
		return AuthFailureReplaced
	default:
		return AuthFailureStorage
	}
}
