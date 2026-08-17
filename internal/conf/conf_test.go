package conf_test

import (
	"net/url"
	"testing"
	"time"

	"glex/internal/conf"

	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/file"
)

func TestExampleAuthConfigurationMatchesConfirmedDefaults(t *testing.T) {
	c := config.New(config.WithSource(file.NewSource("../../configs")))
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Load(); err != nil {
		t.Fatalf("load example config: %v", err)
	}

	var bootstrap conf.Bootstrap
	if err := c.Scan(&bootstrap); err != nil {
		t.Fatalf("scan example config: %v", err)
	}
	auth := bootstrap.GetAuth()
	if auth == nil {
		t.Fatal("auth config is missing")
	}

	if got, want := auth.GetFixedLifetime().AsDuration(), 7*24*time.Hour; got != want {
		t.Errorf("fixed lifetime = %s, want %s", got, want)
	}
	if got, want := auth.GetIdleLifetime().AsDuration(), 30*time.Minute; got != want {
		t.Errorf("idle lifetime = %s, want %s", got, want)
	}
	if got, want := auth.GetMaxSessionsPerUser(), uint32(5); got != want {
		t.Errorf("max sessions per user = %d, want %d", got, want)
	}
	assertCounter(t, "username", auth.GetUsernameLoginCounter(), 5, 10*time.Minute)
	assertCounter(t, "source IP", auth.GetSourceIpLoginCounter(), 20, 10*time.Minute)
	if auth.GetRedisNamespace() != "glex:auth" {
		t.Errorf("Redis auth namespace = %q, want %q", auth.GetRedisNamespace(), "glex:auth")
	}
	if !auth.GetEdgeProxyAuthoritativeSourceIp() {
		t.Error("edge proxy authoritative-source-IP deployment assertion is false")
	}
	databaseURL, err := url.Parse(bootstrap.GetData().GetDatabase().GetSource())
	if err != nil {
		t.Fatalf("parse example database source: %v", err)
	}
	if _, configured := databaseURL.User.Password(); configured {
		t.Error("example database source contains a committed password")
	}
}

func assertCounter(t *testing.T, name string, counter *conf.Auth_LoginCounter, failures uint32, window time.Duration) {
	t.Helper()
	if counter == nil {
		t.Fatalf("%s login counter config is missing", name)
	}
	if got := counter.GetMaxFailures(); got != failures {
		t.Errorf("%s max failures = %d, want %d", name, got, failures)
	}
	if got := counter.GetWindow().AsDuration(); got != window {
		t.Errorf("%s counter window = %s, want %s", name, got, window)
	}
}
