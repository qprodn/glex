package authninfra

import (
	"glex/internal/infra/store"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"glex/internal/conf"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnrepo "glex/internal/domain/authn/repository"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core"
	satokenmanager "github.com/sa-tokens/sa-token-go/core/manager"
	satokensession "github.com/sa-tokens/sa-token-go/core/session"
	"go.einride.tech/aip/ordering"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

const (
	loginSessionKeyPrefix = "login-session:"
	// LoginSessionMetadataPrefix addresses the authoritative metadata of one
	// Login Session. It is exported as part of the Redis key contract: the
	// whole-graph integration suites corrupt these keys deliberately to prove
	// failures classify as backend errors, never as authentication denials.
	LoginSessionMetadataPrefix = loginSessionKeyPrefix + "meta:"
	loginSessionUserKeyPrefix  = loginSessionKeyPrefix + "user:"
	loginSessionTokenKeyPrefix = loginSessionKeyPrefix + "token:"
	loginSessionAllKey         = loginSessionKeyPrefix + "all"
	loginSessionSequenceKey    = loginSessionKeyPrefix + "sequence"
)

var createLoginSessionScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[3]) == 1 or redis.call('EXISTS', KEYS[4]) == 1 then
  return 0
end
local tokenValue = redis.call('GET', KEYS[6])
if not tokenValue or tokenValue == 'KICK_OUT' or tokenValue == 'BE_REPLACED' then
  return -1
end
local decoded, tokenInfo = pcall(cjson.decode, tokenValue)
if not decoded or tostring(tokenInfo.loginId) ~= ARGV[11] or tokenInfo.device ~= ARGV[12] then
  return -1
end

local existing = redis.call('ZRANGE', KEYS[1], 0, -1)
for _, sessionID in ipairs(existing) do
  if redis.call('EXISTS', ARGV[5] .. sessionID) == 0 then
    redis.call('ZREM', KEYS[1], sessionID)
    redis.call('ZREM', KEYS[2], sessionID)
  end
end

local sequence = redis.call('INCR', KEYS[5])
redis.call('SET', KEYS[3], ARGV[2], 'PX', ARGV[3])
-- Keep the credential fingerprint for one additional fixed-lifetime window.
-- After the token and metadata expire it is a non-secret expiry tombstone,
-- allowing middleware to distinguish fixed expiry from an unknown token.
redis.call('SET', KEYS[4], ARGV[1], 'PX', ARGV[13])
redis.call('ZADD', KEYS[1], sequence, ARGV[1])
redis.call('ZADD', KEYS[2], sequence, ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[3])

while redis.call('ZCARD', KEYS[1]) > tonumber(ARGV[4]) do
  local victim = redis.call('ZRANGE', KEYS[1], 0, 0)[1]
  local metadataKey = ARGV[5] .. victim
  local encoded = redis.call('GET', metadataKey)
  redis.call('ZREM', KEYS[1], victim)
  redis.call('ZREM', KEYS[2], victim)
  if encoded then
    local metadata = cjson.decode(encoded)
    redis.call('DEL', metadataKey)
    redis.call('DEL', ARGV[6] .. metadata.fingerprint)
    local tokenKey = ARGV[7] .. metadata.bearer_token
    if redis.call('EXISTS', tokenKey) == 1 then
      redis.call('SET', tokenKey, 'BE_REPLACED', 'KEEPTTL', 'XX')
    end
    local accountKey = ARGV[8] .. tostring(metadata.system_user_id) .. ':' .. metadata.device_type
    if redis.call('GET', accountKey) == metadata.bearer_token then
      redis.call('DEL', accountKey)
    end
    redis.call('DEL', ARGV[9] .. metadata.bearer_token)
    redis.call('DEL', ARGV[10] .. metadata.bearer_token)
  end
end
return 1
`)

var revokeLoginSessionScript = redis.NewScript(`
local encoded = redis.call('GET', ARGV[4] .. ARGV[1])
if not encoded then
  return 0
end
local metadata = cjson.decode(encoded)
if ARGV[2] ~= '' and tostring(metadata.system_user_id) ~= ARGV[2] then
  return -1
end

local userKey = ARGV[6] .. tostring(metadata.system_user_id)
redis.call('ZREM', userKey, ARGV[1])
redis.call('ZREM', ARGV[7], ARGV[1])
redis.call('DEL', ARGV[4] .. ARGV[1])
redis.call('DEL', ARGV[5] .. metadata.fingerprint)

local tokenKey = ARGV[8] .. metadata.bearer_token
if ARGV[3] == 'LOGOUT' then
  redis.call('DEL', tokenKey)
elseif redis.call('EXISTS', tokenKey) == 1 then
  redis.call('SET', tokenKey, ARGV[3], 'KEEPTTL', 'XX')
end
local accountKey = ARGV[9] .. tostring(metadata.system_user_id) .. ':' .. metadata.device_type
if redis.call('GET', accountKey) == metadata.bearer_token then
  redis.call('DEL', accountKey)
end
redis.call('DEL', ARGV[10] .. metadata.bearer_token)
redis.call('DEL', ARGV[11] .. metadata.bearer_token)

local remaining = redis.call('ZRANGE', userKey, 0, -1)
for _, sessionID in ipairs(remaining) do
  if redis.call('EXISTS', ARGV[4] .. sessionID) == 0 then
    redis.call('ZREM', userKey, sessionID)
    redis.call('ZREM', ARGV[7], sessionID)
  end
end
if redis.call('ZCARD', userKey) == 0 then
  redis.call('DEL', userKey)
  redis.call('DEL', ARGV[12] .. tostring(metadata.system_user_id))
end
return 1
`)

var revokeSystemUsersLoginSessionsScript = redis.NewScript(`
local validateIndex = 10
while validateIndex <= #ARGV do
  local userKey = ARGV[3] .. ARGV[validateIndex]
  local sessionIDs = redis.call('ZRANGE', userKey, 0, -1)
  for _, sessionID in ipairs(sessionIDs) do
    local encoded = redis.call('GET', ARGV[1] .. sessionID)
    if encoded then
      local decoded, metadata = pcall(cjson.decode, encoded)
      if not decoded or not metadata.fingerprint or not metadata.bearer_token or not metadata.device_type then
        return redis.error_reply('invalid Login Session metadata')
      end
    end
  end
  validateIndex = validateIndex + 2
end

local revoked = 0
local i = 10
while i <= #ARGV do
  local systemUserID = ARGV[i]
  local exceptSessionID = ARGV[i + 1]
  local userKey = ARGV[3] .. systemUserID
  local sessionIDs = redis.call('ZRANGE', userKey, 0, -1)
  for _, sessionID in ipairs(sessionIDs) do
    local metadataKey = ARGV[1] .. sessionID
    local encoded = redis.call('GET', metadataKey)
    if sessionID == exceptSessionID and not encoded then
      redis.call('ZREM', userKey, sessionID)
      redis.call('ZREM', ARGV[4], sessionID)
    elseif sessionID ~= exceptSessionID then
      redis.call('ZREM', userKey, sessionID)
      redis.call('ZREM', ARGV[4], sessionID)
      if encoded then
        local metadata = cjson.decode(encoded)
        redis.call('DEL', metadataKey)
        redis.call('DEL', ARGV[2] .. metadata.fingerprint)
        local tokenKey = ARGV[5] .. metadata.bearer_token
        if redis.call('EXISTS', tokenKey) == 1 then
          redis.call('SET', tokenKey, 'KICK_OUT', 'KEEPTTL', 'XX')
        end
        local accountKey = ARGV[6] .. systemUserID .. ':' .. metadata.device_type
        if redis.call('GET', accountKey) == metadata.bearer_token then
          redis.call('DEL', accountKey)
        end
        redis.call('DEL', ARGV[7] .. metadata.bearer_token)
        redis.call('DEL', ARGV[8] .. metadata.bearer_token)
        revoked = revoked + 1
      end
    end
  end
  if redis.call('ZCARD', userKey) == 0 then
    redis.call('DEL', userKey)
    redis.call('DEL', ARGV[9] .. systemUserID)
  end
  i = i + 2
end
return revoked
`)

type loginSessionPO struct {
	ID           string    `json:"id"`
	SystemUserID string    `json:"system_user_id"`
	UserName     string    `json:"user_name"`
	DeviceType   string    `json:"device_type"`
	IPAddress    string    `json:"ip_address"`
	Browser      string    `json:"browser"`
	OS           string    `json:"os"`
	LoginTime    time.Time `json:"login_time"`
	BearerToken  string    `json:"bearer_token"`
	Fingerprint  string    `json:"fingerprint"`
}

func newLoginSession(in *authnentity.LoginSession, id, bearerToken string) *loginSessionPO {
	return &loginSessionPO{
		ID:           id,
		SystemUserID: strconv.FormatInt(in.SystemUserID, 10),
		UserName:     in.UserName,
		DeviceType:   in.DeviceType,
		IPAddress:    in.IPAddress,
		Browser:      in.Browser,
		OS:           in.OS,
		LoginTime:    in.LoginTime,
		BearerToken:  bearerToken,
		Fingerprint:  tokenFingerprint(bearerToken),
	}
}

func toBizLoginSession(p *loginSessionPO) (*authnentity.LoginSession, error) {
	systemUserID, err := strconv.ParseInt(p.SystemUserID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("decode System User ID: %w", err)
	}
	return &authnentity.LoginSession{
		ID:           p.ID,
		SystemUserID: systemUserID,
		UserName:     p.UserName,
		DeviceType:   p.DeviceType,
		IPAddress:    p.IPAddress,
		Browser:      p.Browser,
		OS:           p.OS,
		LoginTime:    p.LoginTime,
	}, nil
}

type loginSessionRepo struct {
	rdb          *redis.Client
	logger       *slog.Logger
	manager      *core.Manager
	namespace    string
	readTimeout  time.Duration
	writeTimeout time.Duration
	lifetime     time.Duration
	maxSessions  int
}

// NewLoginSessionRepo constructs the authoritative Redis Login Session store.
func NewLoginSessionRepo(data *store.Data, manager *core.Manager, c *conf.Auth) authnrepo.LoginSessionRepo {
	return &loginSessionRepo{
		rdb:          data.Redis,
		logger:       data.Logger.With(slog.String("component", "infra.authn.login_session")),
		manager:      manager,
		namespace:    normalizeNamespace(c.GetRedisNamespace()),
		readTimeout:  data.RedisReadTimeout,
		writeTimeout: data.RedisWriteTimeout,
		lifetime:     c.GetFixedLifetime().AsDuration(),
		maxSessions:  int(c.GetMaxSessionsPerUser()),
	}
}

func (r *loginSessionRepo) key(suffix string) string { return r.namespace + suffix }

func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func authRedisBackendError(ctx context.Context, logger *slog.Logger, operation string, err error) error {
	logger.WarnContext(ctx, "authentication Redis operation failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return authndomain.ErrAuthBackendUnavailable
}

func (r *loginSessionRepo) Create(ctx context.Context, bearerToken string, session *authnentity.LoginSession) (*authnentity.LoginSession, error) {
	if bearerToken == "" || session == nil || session.SystemUserID <= 0 {
		return nil, authndomain.ErrTokenInvalid
	}
	for attempts := 0; attempts < 3; attempts++ {
		id, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("generate Session ID: %w", err)
		}
		po := newLoginSession(session, id.String(), bearerToken)
		payload, err := json.Marshal(po)
		if err != nil {
			return nil, fmt.Errorf("encode Login Session: %w", err)
		}
		writeCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.writeTimeout, store.DefaultAuthWriteTimeout))
		result, err := createLoginSessionScript.Run(writeCtx, r.rdb, []string{
			r.key(loginSessionUserKeyPrefix + po.SystemUserID),
			r.key(loginSessionAllKey),
			r.key(LoginSessionMetadataPrefix + po.ID),
			r.key(loginSessionTokenKeyPrefix + po.Fingerprint),
			r.key(loginSessionSequenceKey),
			r.key(AuthManagerKeyPrefix + satokenmanager.TokenKeyPrefix + bearerToken),
		},
			po.ID,
			payload,
			r.lifetime.Milliseconds(),
			r.maxSessions,
			r.key(LoginSessionMetadataPrefix),
			r.key(loginSessionTokenKeyPrefix),
			r.key(AuthManagerKeyPrefix+satokenmanager.TokenKeyPrefix),
			r.key(AuthManagerKeyPrefix+satokenmanager.AccountKeyPrefix),
			r.key(AuthManagerKeyPrefix+satokensession.TokenSessionKeyPrefix),
			r.key(AuthManagerKeyPrefix+satokenmanager.RenewKeyPrefix),
			po.SystemUserID,
			po.DeviceType,
			(2 * r.lifetime).Milliseconds(),
		).Int()
		cancel()
		if err != nil {
			return nil, authRedisBackendError(ctx, r.logger, "create_login_session", err)
		}
		if result == 1 {
			return toBizLoginSession(po)
		}
		if result == -1 {
			return nil, authndomain.ErrTokenInvalid
		}
	}
	return nil, fmt.Errorf("generate unique Session ID: collision limit reached")
}

func (r *loginSessionRepo) FindByID(ctx context.Context, sessionID string) (*authnentity.LoginSession, error) {
	po, err := r.findPO(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	session, err := toBizLoginSession(po)
	if err != nil {
		return nil, authRedisBackendError(ctx, r.logger, "decode_system_user_id", err)
	}
	return session, nil
}

func (r *loginSessionRepo) FindByToken(ctx context.Context, bearerToken string) (*authnentity.LoginSession, error) {
	loggedIn, err := r.manager.CheckLoginWithState(bearerToken)
	if err != nil {
		if errors.Is(err, ErrAuthKeyNotFound) {
			return nil, r.missingTokenError(ctx, bearerToken)
		}
		return nil, mapLoginSessionAuthError(err)
	}
	if !loggedIn {
		return nil, authndomain.ErrTokenInvalid
	}
	if err := r.manager.CheckActiveTimeout(bearerToken); err != nil {
		return nil, mapLoginSessionAuthError(err)
	}
	readCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.readTimeout, store.DefaultAuthReadTimeout))
	defer cancel()
	sessionID, err := r.rdb.Get(readCtx, r.key(loginSessionTokenKeyPrefix+tokenFingerprint(bearerToken))).Result()
	if errors.Is(err, redis.Nil) {
		return nil, authndomain.ErrTokenInvalid
	}
	if err != nil {
		return nil, authRedisBackendError(ctx, r.logger, "find_login_session_by_token", err)
	}
	return r.FindByID(ctx, sessionID)
}

func (r *loginSessionRepo) missingTokenError(ctx context.Context, bearerToken string) error {
	readCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.readTimeout, store.DefaultAuthReadTimeout))
	defer cancel()
	_, err := r.rdb.Get(readCtx, r.key(loginSessionTokenKeyPrefix+tokenFingerprint(bearerToken))).Result()
	switch {
	case err == nil:
		return authndomain.ErrTokenExpired
	case errors.Is(err, redis.Nil):
		return authndomain.ErrTokenInvalid
	default:
		return authRedisBackendError(ctx, r.logger, "classify_missing_login_session_token", err)
	}
}

func (r *loginSessionRepo) findPO(ctx context.Context, sessionID string) (*loginSessionPO, error) {
	readCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.readTimeout, store.DefaultAuthReadTimeout))
	defer cancel()
	encoded, err := r.rdb.Get(readCtx, r.key(LoginSessionMetadataPrefix+sessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, authndomain.ErrLoginSessionNotFound
	}
	if err != nil {
		return nil, authRedisBackendError(ctx, r.logger, "find_login_session_by_id", err)
	}
	var po loginSessionPO
	if err := json.Unmarshal([]byte(encoded), &po); err != nil {
		return nil, authndomain.ErrAuthBackendUnavailable
	}
	return &po, nil
}

func mapLoginSessionAuthError(err error) error {
	if errors.Is(err, ErrAuthKeyNotFound) {
		return authndomain.ErrTokenInvalid
	}
	switch ClassifyAuthFailure(err) {
	case AuthFailureInvalidToken, AuthFailureNotLogin:
		return authndomain.ErrTokenInvalid
	case AuthFailureExpired:
		return authndomain.ErrTokenExpired
	case AuthFailureIdleTimeout:
		return authndomain.ErrTokenIdleTimeout
	case AuthFailureKickout:
		return authndomain.ErrLoginSessionKickedOut
	case AuthFailureReplaced:
		return authndomain.ErrLoginSessionReplaced
	default:
		return authndomain.ErrAuthBackendUnavailable
	}
}

func (r *loginSessionRepo) List(ctx context.Context, opts ...authnrepo.ListOption) ([]*authnentity.LoginSession, error) {
	return r.list(ctx, r.key(loginSessionAllKey), opts...)
}

func (r *loginSessionRepo) ListBySystemUser(ctx context.Context, systemUserID int64, opts ...authnrepo.ListOption) ([]*authnentity.LoginSession, error) {
	return r.list(ctx, r.key(loginSessionUserKeyPrefix+strconv.FormatInt(systemUserID, 10)), opts...)
}

func (r *loginSessionRepo) list(ctx context.Context, indexKey string, opts ...authnrepo.ListOption) ([]*authnentity.LoginSession, error) {
	o := authnrepo.ListOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	readCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.readTimeout, store.DefaultAuthReadTimeout))
	defer cancel()
	ids, err := r.rdb.ZRange(readCtx, indexKey, 0, -1).Result()
	if err != nil {
		return nil, authRedisBackendError(ctx, r.logger, "list_login_session_index", err)
	}
	if len(ids) == 0 {
		return []*authnentity.LoginSession{}, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = r.key(LoginSessionMetadataPrefix + id)
	}
	values, err := r.rdb.MGet(readCtx, keys...).Result()
	if err != nil {
		return nil, authRedisBackendError(ctx, r.logger, "list_login_session_metadata", err)
	}
	result := make([]*authnentity.LoginSession, 0, len(values))
	staleIDs := make([]any, 0)
	for i, value := range values {
		encoded, ok := value.(string)
		if !ok {
			staleIDs = append(staleIDs, ids[i])
			continue
		}
		var po loginSessionPO
		if err := json.Unmarshal([]byte(encoded), &po); err != nil {
			return nil, authndomain.ErrAuthBackendUnavailable
		}
		session, err := toBizLoginSession(&po)
		if err != nil {
			return nil, authRedisBackendError(ctx, r.logger, "decode_system_user_id", err)
		}
		matches, err := matchesLoginSessionFilter(o.Filter.CheckedExpr.GetExpr(), session)
		if err != nil {
			return nil, err
		}
		if matches {
			result = append(result, session)
		}
	}
	if len(staleIDs) > 0 {
		writeCtx, writeCancel := context.WithTimeout(ctx, store.BoundedTimeout(r.writeTimeout, store.DefaultAuthWriteTimeout))
		err := r.rdb.ZRem(writeCtx, indexKey, staleIDs...).Err()
		writeCancel()
		if err != nil {
			return nil, authRedisBackendError(ctx, r.logger, "clean_stale_login_session_index", err)
		}
	}
	if len(o.OrderBy.Fields) > 0 {
		sort.SliceStable(result, func(i, j int) bool {
			return lessLoginSession(result[i], result[j], o.OrderBy.Fields)
		})
	}
	start := o.Offset
	if start < 0 {
		start = 0
	}
	if start >= len(result) {
		return []*authnentity.LoginSession{}, nil
	}
	end := len(result)
	if o.Limit > 0 && start+o.Limit < end {
		end = start + o.Limit
	}
	return result[start:end], nil
}

func matchesLoginSessionFilter(expression *expr.Expr, session *authnentity.LoginSession) (bool, error) {
	if expression == nil {
		return true, nil
	}
	call := expression.GetCallExpr()
	if call == nil {
		return false, fmt.Errorf("unsupported Login Session filter expression")
	}
	args := call.GetArgs()
	switch call.GetFunction() {
	case "_&&_", "_||_", "AND", "OR":
		if len(args) != 2 {
			return false, fmt.Errorf("invalid Login Session boolean filter")
		}
		left, err := matchesLoginSessionFilter(args[0], session)
		if err != nil {
			return false, err
		}
		right, err := matchesLoginSessionFilter(args[1], session)
		if err != nil {
			return false, err
		}
		if call.GetFunction() == "_&&_" || call.GetFunction() == "AND" {
			return left && right, nil
		}
		return left || right, nil
	case "!_", "NOT":
		if len(args) != 1 {
			return false, fmt.Errorf("invalid Login Session negation filter")
		}
		matched, err := matchesLoginSessionFilter(args[0], session)
		return !matched, err
	case "_==_", "_!=_", "=", "!=":
		if len(args) != 2 {
			return false, fmt.Errorf("invalid Login Session comparison filter")
		}
		left, leftOK := loginSessionFilterString(args[0], session)
		right, rightOK := loginSessionFilterString(args[1], session)
		if !leftOK || !rightOK {
			return false, fmt.Errorf("Login Session filters support string equality for user_name and ipaddr")
		}
		equal := left == right
		if call.GetFunction() == "_!=_" || call.GetFunction() == "!=" {
			equal = !equal
		}
		return equal, nil
	default:
		return false, fmt.Errorf("unsupported Login Session filter function %q", call.GetFunction())
	}
}

func loginSessionFilterString(expression *expr.Expr, session *authnentity.LoginSession) (string, bool) {
	if identifier := expression.GetIdentExpr(); identifier != nil {
		switch identifier.GetName() {
		case "user_name":
			return session.UserName, true
		case "ipaddr":
			return session.IPAddress, true
		default:
			return "", false
		}
	}
	constant := expression.GetConstExpr()
	if constant == nil {
		return "", false
	}
	if _, ok := constant.GetConstantKind().(*expr.Constant_StringValue); !ok {
		return "", false
	}
	return constant.GetStringValue(), true
}

func lessLoginSession(left, right *authnentity.LoginSession, fields []ordering.Field) bool {
	for _, field := range fields {
		comparison := compareLoginSessionField(left, right, field.Path)
		if comparison == 0 {
			continue
		}
		if field.Desc {
			return comparison > 0
		}
		return comparison < 0
	}
	return left.ID < right.ID
}

func compareLoginSessionField(left, right *authnentity.LoginSession, field string) int {
	switch field {
	case "login_time":
		return left.LoginTime.Compare(right.LoginTime)
	case "token_id":
		return compareString(left.ID, right.ID)
	case "user_name":
		return compareString(left.UserName, right.UserName)
	case "ipaddr":
		return compareString(left.IPAddress, right.IPAddress)
	case "device_type":
		return compareString(left.DeviceType, right.DeviceType)
	default:
		return 0
	}
}

func compareString(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func (r *loginSessionRepo) Owns(ctx context.Context, systemUserID int64, sessionID string) (bool, error) {
	po, err := r.findPO(ctx, sessionID)
	if errors.Is(err, authndomain.ErrLoginSessionNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return po.SystemUserID == strconv.FormatInt(systemUserID, 10), nil
}

func (r *loginSessionRepo) Logout(ctx context.Context, sessionID string) error {
	return r.revoke(ctx, sessionID, 0, "LOGOUT")
}

func (r *loginSessionRepo) LogoutOwned(ctx context.Context, systemUserID int64, sessionID string) error {
	return r.revoke(ctx, sessionID, systemUserID, "LOGOUT")
}

func (r *loginSessionRepo) Kickout(ctx context.Context, sessionID string) error {
	return r.revoke(ctx, sessionID, 0, "KICK_OUT")
}

func (r *loginSessionRepo) Replace(ctx context.Context, sessionID string) error {
	return r.revoke(ctx, sessionID, 0, "BE_REPLACED")
}

func (r *loginSessionRepo) RevokeSystemUser(ctx context.Context, systemUserID int64, exceptSessionID string) error {
	return r.RevokeSystemUsers(ctx, []int64{systemUserID}, exceptSessionID)
}

func (r *loginSessionRepo) RevokeSystemUsers(ctx context.Context, systemUserIDs []int64, exceptSessionID string) error {
	if len(systemUserIDs) == 0 {
		return nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.writeTimeout, store.DefaultAuthWriteTimeout))
	defer cancel()
	args := []any{
		r.key(LoginSessionMetadataPrefix),
		r.key(loginSessionTokenKeyPrefix),
		r.key(loginSessionUserKeyPrefix),
		r.key(loginSessionAllKey),
		r.key(AuthManagerKeyPrefix + satokenmanager.TokenKeyPrefix),
		r.key(AuthManagerKeyPrefix + satokenmanager.AccountKeyPrefix),
		r.key(AuthManagerKeyPrefix + satokensession.TokenSessionKeyPrefix),
		r.key(AuthManagerKeyPrefix + satokenmanager.RenewKeyPrefix),
		r.key(AuthManagerKeyPrefix + satokensession.AccountSessionKeyPrefix),
	}
	for _, systemUserID := range systemUserIDs {
		args = append(args, strconv.FormatInt(systemUserID, 10), exceptSessionID)
	}
	_, err := revokeSystemUsersLoginSessionsScript.Run(writeCtx, r.rdb, nil, args...).Result()
	if err != nil {
		return authRedisBackendError(ctx, r.logger, "revoke_system_users_login_sessions", err)
	}
	return nil
}

func (r *loginSessionRepo) revoke(ctx context.Context, sessionID string, expectedSystemUserID int64, state string) error {
	expectedOwner := ""
	if expectedSystemUserID != 0 {
		expectedOwner = strconv.FormatInt(expectedSystemUserID, 10)
	}
	writeCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.writeTimeout, store.DefaultAuthWriteTimeout))
	defer cancel()
	result, err := revokeLoginSessionScript.Run(writeCtx, r.rdb, nil,
		sessionID,
		expectedOwner,
		state,
		r.key(LoginSessionMetadataPrefix),
		r.key(loginSessionTokenKeyPrefix),
		r.key(loginSessionUserKeyPrefix),
		r.key(loginSessionAllKey),
		r.key(AuthManagerKeyPrefix+satokenmanager.TokenKeyPrefix),
		r.key(AuthManagerKeyPrefix+satokenmanager.AccountKeyPrefix),
		r.key(AuthManagerKeyPrefix+satokensession.TokenSessionKeyPrefix),
		r.key(AuthManagerKeyPrefix+satokenmanager.RenewKeyPrefix),
		r.key(AuthManagerKeyPrefix+satokensession.AccountSessionKeyPrefix),
	).Int()
	if err != nil {
		return authRedisBackendError(ctx, r.logger, "revoke_login_session", err)
	}
	if result == -1 {
		return authndomain.ErrLoginSessionNotOwned
	}
	return nil
}
