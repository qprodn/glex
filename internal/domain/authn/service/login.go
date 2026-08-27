package service

import (
	"context"

	"glex/internal/domain/authn"
	"glex/internal/domain/authn/repository"
)

// RecordInvalidLogin records one failed attempt on both failure counters and
// normalizes the outcome: a locked counter reports the lock (with its
// remaining duration), otherwise the generic invalid-credentials reason. The
// caller deliberately cannot distinguish which counter locked.
func RecordInvalidLogin(ctx context.Context, counterRepo repository.LoginCounterRepo, userName, sourceIP string) error {
	remaining, err := counterRepo.RecordFailure(ctx, userName, sourceIP)
	if err != nil {
		return err
	}
	if remaining > 0 {
		return authn.NewLoginLocked(remaining)
	}
	return authn.ErrInvalidCredentials
}
