package repository

import (
	"context"

	"glex/internal/domain/authn/entity"
)

// LoginChallengeRepo stores Login Challenges. Issue returns the picture and
// its id; the answer remains stored. Consume removes the stored answer and
// returns it, so a second consume cannot observe it. An absent answer — unknown
// id, expiry, or a challenge already spent — is ErrLoginChallengeInvalid, not
// a backend failure.
type LoginChallengeRepo interface {
	Issue(ctx context.Context) (*entity.LoginChallenge, error)
	Consume(ctx context.Context, challengeID string) (string, error)
}
