package authninfra

import (
	"bytes"
	"errors"
	"image/png"
	"strings"
	"testing"
	"time"

	"glex/internal/infra/store"

	authndomain "glex/internal/domain/authn"
	authnrepo "glex/internal/domain/authn/repository"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestLoginChallengeRepo(t *testing.T) (authnrepo.LoginChallengeRepo, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	data := &store.Data{Redis: rdb, Logger: testLogger(), RedisWriteTimeout: time.Second}
	return NewLoginChallengeRepo(data, testAuthConf()), mr, rdb
}

func TestLoginChallengeIssueStoresAPictureAndAFourCharacterAnswer(t *testing.T) {
	repo, _, rdb := newTestLoginChallengeRepo(t)
	issued, err := repo.Issue(t.Context())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if issued.ID == "" {
		t.Fatal("Issue returned an empty id")
	}
	img, err := png.Decode(bytes.NewReader(issued.Image))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	if img.Bounds().Dx() != challengeImageWidth || img.Bounds().Dy() != challengeImageHeight {
		t.Fatalf("image bounds = %v, want %dx%d", img.Bounds(), challengeImageWidth, challengeImageHeight)
	}

	keys, err := rdb.Keys(t.Context(), "*").Result()
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 1 || !strings.HasSuffix(keys[0], issued.ID) {
		t.Fatalf("keys = %v, want the issued id", keys)
	}
	stored, err := rdb.Get(t.Context(), keys[0]).Result()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(stored) != loginChallengeLength {
		t.Fatalf("stored answer %q, want %d characters", stored, loginChallengeLength)
	}
	for _, r := range stored {
		if !strings.ContainsRune(loginChallengeAlphabet, r) {
			t.Fatalf("stored answer %q contains %q outside the alphabet", stored, r)
		}
	}
	ttl, err := rdb.TTL(t.Context(), keys[0]).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > loginChallengeTTL {
		t.Fatalf("TTL = %v, want within %v", ttl, loginChallengeTTL)
	}
}

func TestLoginChallengeConsumeSucceedsOnceAndLeavesItsSibling(t *testing.T) {
	repo, _, _ := newTestLoginChallengeRepo(t)
	ctx := t.Context()
	first, err := repo.Issue(ctx)
	if err != nil {
		t.Fatalf("Issue first: %v", err)
	}
	second, err := repo.Issue(ctx)
	if err != nil {
		t.Fatalf("Issue second: %v", err)
	}
	answer, err := repo.Consume(ctx, first.ID)
	if err != nil {
		t.Fatalf("Consume first: %v", err)
	}
	if len(answer) != loginChallengeLength {
		t.Fatalf("answer %q, want %d characters", answer, loginChallengeLength)
	}
	if _, err := repo.Consume(ctx, first.ID); !errors.Is(err, authndomain.ErrLoginChallengeInvalid) {
		t.Fatalf("second Consume = %v, want ErrLoginChallengeInvalid", err)
	}
	if _, err := repo.Consume(ctx, second.ID); err != nil {
		t.Fatalf("sibling Consume: %v", err)
	}
}

func TestLoginChallengeUnknownIDIsInvalid(t *testing.T) {
	repo, _, _ := newTestLoginChallengeRepo(t)
	_, err := repo.Consume(t.Context(), "missing")
	if !errors.Is(err, authndomain.ErrLoginChallengeInvalid) {
		t.Fatalf("Consume = %v, want ErrLoginChallengeInvalid", err)
	}
}

func TestLoginChallengeExpires(t *testing.T) {
	repo, mr, _ := newTestLoginChallengeRepo(t)
	issued, err := repo.Issue(t.Context())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	mr.FastForward(loginChallengeTTL + time.Millisecond)
	if _, err := repo.Consume(t.Context(), issued.ID); !errors.Is(err, authndomain.ErrLoginChallengeInvalid) {
		t.Fatalf("expired Consume = %v, want ErrLoginChallengeInvalid", err)
	}
}

func TestLoginChallengeBackendFailureMapsToServiceUnavailable(t *testing.T) {
	repo, mr, _ := newTestLoginChallengeRepo(t)
	mr.Close()
	if _, err := repo.Issue(t.Context()); !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Issue error = %v, want ErrAuthBackendUnavailable", err)
	}
	if _, err := repo.Consume(t.Context(), "any"); !errors.Is(err, authndomain.ErrAuthBackendUnavailable) {
		t.Fatalf("Consume error = %v, want ErrAuthBackendUnavailable", err)
	}
}
