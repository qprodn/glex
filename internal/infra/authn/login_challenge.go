package authninfra

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"time"

	"glex/internal/conf"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnrepo "glex/internal/domain/authn/repository"
	"glex/internal/infra/store"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	// Digits and uppercase letters, without the glyphs that look alike.
	loginChallengeAlphabet  = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	loginChallengeLength    = 4
	loginChallengeTTL       = 2 * time.Minute
	loginChallengeKeyPrefix = "login-challenge:"
)

type loginChallengeRepo struct {
	rdb          *redis.Client
	logger       *slog.Logger
	namespace    string
	writeTimeout time.Duration
}

// NewLoginChallengeRepo stores Login Challenges in Redis. The id is the
// capability; the answer is the stored secret and never leaves this adapter
// except as the picture drawn for the caller.
func NewLoginChallengeRepo(data *store.Data, c *conf.Auth) authnrepo.LoginChallengeRepo {
	return &loginChallengeRepo{
		rdb:          data.Redis,
		logger:       data.Logger.With(slog.String("component", "infra.authn.login_challenge")),
		namespace:    normalizeNamespace(c.GetRedisNamespace()),
		writeTimeout: data.RedisWriteTimeout,
	}
}

func (r *loginChallengeRepo) Issue(ctx context.Context) (*authnentity.LoginChallenge, error) {
	code, err := newLoginChallengeCode()
	if err != nil {
		return nil, authSessionLocalError(ctx, r.logger, "authn.issue_login_challenge", err)
	}
	png, err := renderLoginChallenge(code)
	if err != nil {
		return nil, authSessionLocalError(ctx, r.logger, "authn.issue_login_challenge", err)
	}
	id := uuid.NewString()
	writeCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.writeTimeout, store.DefaultAuthWriteTimeout))
	defer cancel()
	if err := r.rdb.Set(writeCtx, r.key(id), code, loginChallengeTTL).Err(); err != nil {
		return nil, authRedisBackendError(ctx, r.logger, "authn.issue_login_challenge", err)
	}
	return &authnentity.LoginChallenge{ID: id, Image: png}, nil
}

func (r *loginChallengeRepo) Consume(ctx context.Context, challengeID string) (string, error) {
	writeCtx, cancel := context.WithTimeout(ctx, store.BoundedTimeout(r.writeTimeout, store.DefaultAuthWriteTimeout))
	defer cancel()
	answer, err := r.rdb.GetDel(writeCtx, r.key(challengeID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", authndomain.ErrLoginChallengeInvalid
		}
		return "", authRedisBackendError(ctx, r.logger, "authn.consume_login_challenge", err)
	}
	return answer, nil
}

func (r *loginChallengeRepo) key(id string) string {
	return r.namespace + loginChallengeKeyPrefix + id
}

func newLoginChallengeCode() (string, error) {
	out := make([]byte, loginChallengeLength)
	n := len(loginChallengeAlphabet)
	// Drop the remainder so the alphabet is not biased toward its first symbols.
	limit := 256 - (256 % n)
	var b [1]byte
	for i := 0; i < loginChallengeLength; {
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		if int(b[0]) >= limit {
			continue
		}
		out[i] = loginChallengeAlphabet[int(b[0])%n]
		i++
	}
	return string(out), nil
}
