package payinfra

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"glex/internal/conf"
	"glex/internal/infra/store"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func testLimiter(t *testing.T, perMinute int64) (*NotifyRateLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	l := &NotifyRateLimiter{
		rdb:       rdb,
		perMinute: perMinute,
		logger:    testLogger(),
	}
	return l, mr
}

func TestNotifyRateLimiterBlocksOverLimit(t *testing.T) {
	l, _ := testLimiter(t, 2)
	next := http.NotFoundHandler()
	h := l.Filter(next)
	// First two deliveries pass, the third is blocked within the window.
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/pay/notify/wechat/42", nil))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("delivery %d blocked", i+1)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/pay/notify/wechat/42", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third delivery status = %d, want 429", rec.Code)
	}
}

func TestNotifyRateLimiterIsPerMerchant(t *testing.T) {
	l, _ := testLimiter(t, 1)
	h := l.Filter(http.NotFoundHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/pay/notify/wechat/1", nil))
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("merchant 1 blocked on first delivery")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/pay/notify/wechat/2", nil))
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("merchant 2 starved by merchant 1's traffic")
	}
}

func TestNotifyRateLimiterWindowReopens(t *testing.T) {
	l, mr := testLimiter(t, 1)
	h := l.Filter(http.NotFoundHandler())
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/pay/notify/wechat/7", nil))
		if i == 0 && rec.Code == http.StatusTooManyRequests {
			t.Fatal("first delivery blocked")
		}
		if i == 1 && rec.Code != http.StatusTooManyRequests {
			t.Fatal("second delivery within window not blocked")
		}
	}
	mr.FastForward(2 * time.Minute)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/pay/notify/wechat/7", nil))
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("window did not reopen")
	}
}

func TestNotifyRateLimiterFailsOpenOnRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close() // dead endpoint
	l := &NotifyRateLimiter{
		rdb:       redis.NewClient(&redis.Options{Addr: addr, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond, DialTimeout: 100 * time.Millisecond}),
		perMinute: 1,
		logger:    testLogger(),
	}
	rec := httptest.NewRecorder()
	l.Filter(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest("POST", "/pay/notify/wechat/9", nil))
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("redis outage swallowed a payment confirmation")
	}
}

func TestNewNotifyRateLimiterDefaults(t *testing.T) {
	mr := miniredis.RunT(t)
	l := NewNotifyRateLimiter(
		&store.Data{Redis: redis.NewClient(&redis.Options{Addr: mr.Addr()}), Logger: testLogger()},
		&conf.Pay{},
	)
	if l.perMinute != defaultNotifyRatePerMinute {
		t.Fatalf("perMinute = %d, want default", l.perMinute)
	}
}
