package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"glex/internal/biz"
	"glex/internal/conf"

	"github.com/redis/go-redis/v9"
)

const loginFailureKeyPrefix = "login-failure:"

var checkLoginCountersScript = redis.NewScript(`
local longest = 0
for index, key in ipairs(KEYS) do
  local count = tonumber(redis.call('GET', key) or '0')
  if count >= tonumber(ARGV[index]) then
    local ttl = redis.call('PTTL', key)
    if ttl > longest then longest = ttl end
  end
end
return longest
`)

var recordLoginFailureScript = redis.NewScript(`
local longest = 0
for index, key in ipairs(KEYS) do
  local count = redis.call('INCR', key)
  if count == 1 then redis.call('PEXPIRE', key, ARGV[index + 2]) end
  if count >= tonumber(ARGV[index]) then
    local ttl = redis.call('PTTL', key)
    if ttl > longest then longest = ttl end
  end
end
return longest
`)

type loginCounterRepo struct {
	rdb            *redis.Client
	namespace      string
	writeTimeout   time.Duration
	userNameLimit  int64
	userNameWindow time.Duration
	sourceIPLimit  int64
	sourceIPWindow time.Duration
}

// NewLoginCounterRepo constructs the two Redis-backed login failure counters.
// Counter subjects are SHA-256 fingerprints so usernames and source IPs do not
// become visible Redis key material.
func NewLoginCounterRepo(data *Data, c *conf.Auth) biz.LoginCounterRepo {
	return &loginCounterRepo{
		rdb:            data.rdb,
		namespace:      normalizeNamespace(c.GetRedisNamespace()),
		writeTimeout:   data.redisWriteTimeout,
		userNameLimit:  int64(c.GetUsernameLoginCounter().GetMaxFailures()),
		userNameWindow: c.GetUsernameLoginCounter().GetWindow().AsDuration(),
		sourceIPLimit:  int64(c.GetSourceIpLoginCounter().GetMaxFailures()),
		sourceIPWindow: c.GetSourceIpLoginCounter().GetWindow().AsDuration(),
	}
}

func (r *loginCounterRepo) LockRemaining(ctx context.Context, userName, sourceIP string) (time.Duration, error) {
	writeCtx, cancel := context.WithTimeout(ctx, boundedTimeout(r.writeTimeout, defaultAuthWriteTimeout))
	defer cancel()
	milliseconds, err := checkLoginCountersScript.Run(writeCtx, r.rdb, r.keys(userName, sourceIP),
		r.userNameLimit, r.sourceIPLimit,
	).Int64()
	if err != nil {
		return 0, authRedisBackendError("check_login_counters", err)
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func (r *loginCounterRepo) RecordFailure(ctx context.Context, userName, sourceIP string) (time.Duration, error) {
	writeCtx, cancel := context.WithTimeout(ctx, boundedTimeout(r.writeTimeout, defaultAuthWriteTimeout))
	defer cancel()
	milliseconds, err := recordLoginFailureScript.Run(writeCtx, r.rdb, r.keys(userName, sourceIP),
		r.userNameLimit, r.sourceIPLimit,
		r.userNameWindow.Milliseconds(), r.sourceIPWindow.Milliseconds(),
	).Int64()
	if err != nil {
		return 0, authRedisBackendError("record_login_failure", err)
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func (r *loginCounterRepo) ClearUserName(ctx context.Context, userName string) error {
	writeCtx, cancel := context.WithTimeout(ctx, boundedTimeout(r.writeTimeout, defaultAuthWriteTimeout))
	defer cancel()
	if err := r.rdb.Del(writeCtx, r.userNameKey(userName)).Err(); err != nil {
		return authRedisBackendError("clear_username_login_counter", err)
	}
	return nil
}

func (r *loginCounterRepo) keys(userName, sourceIP string) []string {
	return []string{r.userNameKey(userName), r.sourceIPKey(sourceIP)}
}

func (r *loginCounterRepo) userNameKey(userName string) string {
	return r.namespace + loginFailureKeyPrefix + "username:" + loginCounterSubject(userName)
}

func (r *loginCounterRepo) sourceIPKey(sourceIP string) string {
	return r.namespace + loginFailureKeyPrefix + "source-ip:" + loginCounterSubject(sourceIP)
}

func loginCounterSubject(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:])
}
