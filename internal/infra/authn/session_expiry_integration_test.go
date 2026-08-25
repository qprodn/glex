package authninfra

import (
	"errors"
	"testing"
	"time"

	"glex/internal/conf"
	authndomain "glex/internal/domain/authn"

	"google.golang.org/protobuf/types/known/durationpb"
)

// These tests pin the two lifetimes against real Redis expiry with real
// clocks. The Manager works in whole seconds (Timeout/ActiveTimeout are
// int64 seconds), so every lifetime here is a whole number of seconds and
// every wait overshoots the boundary by more than a second to tolerate the
// second-granularity truncation of the stored active timestamp.

// TestFixedLifetimeExpiresDespiteActivity proves the fixed lifetime cannot be
// renewed by activity: a Session kept continuously active still dies at its
// fixed expiry with the distinct expired reason, never the idle reason.
func TestFixedLifetimeExpiresDespiteActivityIntegration(t *testing.T) {
	stores := newIntegrationStores(t, func(c *conf.Auth) {
		c.FixedLifetime = durationpb.New(4 * time.Second)
		// Idle is one second below the fixed lifetime so an idle timeout can
		// never fire first, even under scheduler stretch.
		c.IdleLifetime = durationpb.New(3 * time.Second)
	})
	g := stores.newGraph(t)
	uid := newIntegrationSeeder().user(t, g, 0)
	_, token := loginOnGraph(t, g, uid, "desktop")

	var failure error
	for range 12 {
		time.Sleep(600 * time.Millisecond)
		if _, err := g.authorizer.Authenticate(t.Context(), token); err != nil {
			failure = err
			break
		}
	}
	if failure == nil {
		t.Fatal("continuously active Session outlived its fixed lifetime")
	}
	if !isDistinctUnauthorized(failure, authndomain.ErrTokenExpired) {
		t.Fatalf("fixed-expiry failure = %v, want ErrTokenExpired", failure)
	}
	if _, err := g.sessions.FindByToken(t.Context(), token); !isDistinctUnauthorized(err, authndomain.ErrTokenExpired) {
		t.Fatalf("FindByToken after fixed expiry = %v, want ErrTokenExpired", err)
	}
	if listed, err := g.sessions.ListBySystemUser(t.Context(), uid); err != nil || len(listed) != 0 {
		t.Fatalf("index after fixed expiry = %v/%v, want empty", listed, err)
	}
}

// TestIdleLifetimeExpiresWithoutActivity proves an untouched Session is
// rejected by the explicit active-timeout check while its fixed lifetime is
// still far away.
func TestIdleLifetimeExpiresWithoutActivityIntegration(t *testing.T) {
	stores := newIntegrationStores(t, func(c *conf.Auth) {
		c.FixedLifetime = durationpb.New(60 * time.Second)
		c.IdleLifetime = durationpb.New(2 * time.Second)
	})
	g := stores.newGraph(t)
	uid := newIntegrationSeeder().user(t, g, 0)
	_, token := loginOnGraph(t, g, uid, "desktop")

	if _, err := g.authorizer.Authenticate(t.Context(), token); err != nil {
		t.Fatalf("Authenticate before idle window: %v", err)
	}
	time.Sleep(3200 * time.Millisecond)

	if _, err := g.sessions.FindByToken(t.Context(), token); !isDistinctUnauthorized(err, authndomain.ErrTokenIdleTimeout) {
		t.Fatalf("FindByToken after idle = %v, want ErrTokenIdleTimeout", err)
	}
	if _, err := g.authorizer.Authenticate(t.Context(), token); !isDistinctUnauthorized(err, authndomain.ErrTokenIdleTimeout) {
		t.Fatalf("Authenticate after idle = %v, want ErrTokenIdleTimeout", err)
	}
}

// TestActivityRefreshPreventsIdleTimeout proves each authentication refreshes
// the active timestamp: a Session used regularly survives far beyond its idle
// lifetime, then times out once activity stops.
func TestActivityRefreshPreventsIdleTimeoutIntegration(t *testing.T) {
	stores := newIntegrationStores(t, func(c *conf.Auth) {
		c.FixedLifetime = durationpb.New(60 * time.Second)
		c.IdleLifetime = durationpb.New(2 * time.Second)
	})
	g := stores.newGraph(t)
	uid := newIntegrationSeeder().user(t, g, 0)
	_, token := loginOnGraph(t, g, uid, "desktop")

	// 3.5s of activity in 700ms beats — well past the 2s idle lifetime.
	for range 5 {
		time.Sleep(700 * time.Millisecond)
		if _, err := g.authorizer.Authenticate(t.Context(), token); err != nil {
			t.Fatalf("Authenticate during sustained activity: %v", err)
		}
	}
	time.Sleep(3200 * time.Millisecond)
	if _, err := g.authorizer.Authenticate(t.Context(), token); !errors.Is(err, authndomain.ErrTokenIdleTimeout) {
		t.Fatalf("Authenticate after activity stopped = %v, want ErrTokenIdleTimeout", err)
	}
}
