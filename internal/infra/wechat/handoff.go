package wechatinfra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	wechat "glex/internal/domain/wechat"
	wechatrepo "glex/internal/domain/wechat/repository"
	"glex/internal/infra/store"

	"github.com/redis/go-redis/v9"
)

// handoffKeyPrefix keeps Handoff Codes out of the Login Challenge key
// space. Keys are not given an expiry: this ticket does not choose a
// lifetime. Consume deletes the key, which is the single-use rule.
const handoffKeyPrefix = "wechat:handoff:"

type handoffRepo struct {
	rdb          *redis.Client
	logger       *slog.Logger
	writeTimeout time.Duration
}

type handoffPayload struct {
	OpenID    string `json:"openid"`
	UnionID   string `json:"unionid"`
	Nickname  string `json:"nickname,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Profile   bool   `json:"profile"`
}

// NewHandoffCodeRepo stores Handoff Codes in Redis. The code is the
// capability. The grant is not a WeChat User.
func NewHandoffCodeRepo(data *store.Data) wechatrepo.HandoffCodeRepo {
	logger := data.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &handoffRepo{
		rdb:          data.Redis,
		logger:       logger.With(slog.String("component", "infra.wechat.handoff")),
		writeTimeout: data.RedisWriteTimeout,
	}
}

func (r *handoffRepo) Issue(ctx context.Context, grant wechatrepo.HandoffGrant) (string, error) {
	if r.rdb == nil {
		return "", r.storeError(ctx, "wechat.handoff.issue", errors.New("redis client is absent"))
	}
	code, err := newHandoffCode()
	if err != nil {
		return "", r.storeError(ctx, "wechat.handoff.issue", err)
	}
	raw, err := json.Marshal(handoffPayload{
		OpenID:    grant.OpenID,
		UnionID:   grant.UnionID,
		Nickname:  grant.Nickname,
		AvatarURL: grant.AvatarURL,
		Profile:   grant.Profile,
	})
	if err != nil {
		return "", r.storeError(ctx, "wechat.handoff.issue", err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.writeTimeout, store.DefaultAuthWriteTimeout))
	defer cancel()
	// Expiration 0 means the key stays until Consume deletes it.
	if err := r.rdb.Set(writeCtx, handoffKeyPrefix+code, raw, 0).Err(); err != nil {
		return "", r.storeError(ctx, "wechat.handoff.issue", err)
	}
	return code, nil
}

func (r *handoffRepo) Consume(ctx context.Context, code string) (wechatrepo.HandoffGrant, error) {
	if code == "" {
		return wechatrepo.HandoffGrant{}, wechat.ErrHandoffCodeInvalid
	}
	if r.rdb == nil {
		return wechatrepo.HandoffGrant{}, r.storeError(ctx, "wechat.handoff.consume", errors.New("redis client is absent"))
	}
	writeCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.writeTimeout, store.DefaultAuthWriteTimeout))
	defer cancel()
	raw, err := r.rdb.GetDel(writeCtx, handoffKeyPrefix+code).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return wechatrepo.HandoffGrant{}, wechat.ErrHandoffCodeInvalid
		}
		return wechatrepo.HandoffGrant{}, r.storeError(ctx, "wechat.handoff.consume", err)
	}
	var payload handoffPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return wechatrepo.HandoffGrant{}, r.storeError(ctx, "wechat.handoff.consume", err)
	}
	return wechatrepo.HandoffGrant{
		OpenID:    payload.OpenID,
		UnionID:   payload.UnionID,
		Nickname:  payload.Nickname,
		AvatarURL: payload.AvatarURL,
		Profile:   payload.Profile,
	}, nil
}

func (r *handoffRepo) storeError(ctx context.Context, operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return wechat.ErrWechatStoreUnavailable
	}
	r.logger.ErrorContext(ctx, "wechat handoff store failed",
		slog.String("operation", operation),
		slog.Any("error", err),
	)
	return wechat.ErrWechatStoreUnavailable
}

func newHandoffCode() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
