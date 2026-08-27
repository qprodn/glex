// Package service holds the System User module's own rules and intra-domain
// orchestration: policies that need no other module and no transaction
// boundary. Cross-domain orchestration (transactions, credentials, session
// revocation) stays with the Application service.
package service

import (
	"context"
	"errors"

	"glex/internal/domain/user"
	"glex/internal/domain/user/entity"
	"glex/internal/domain/user/repository"
)

// AssertUnique checks user_name (always) and email/phone_number (when set) are
// not already taken, ignoring the record with excludeID (0 = none). A
// not-found from the repo is the expected "available" case. Checks run against
// active rows only, so soft-deleted values are reusable.
func AssertUnique(ctx context.Context, repo repository.UserRepo, u *entity.User, excludeID int64) error {
	if existing, err := repo.FindByUserName(ctx, u.UserName); err != nil {
		if !errors.Is(err, user.ErrUserNotFound) {
			return err
		}
	} else if existing != nil && existing.ID != excludeID {
		return user.ErrUserNameConflict
	}
	if u.Email != "" {
		if existing, err := repo.FindByEmail(ctx, u.Email); err != nil {
			if !errors.Is(err, user.ErrUserNotFound) {
				return err
			}
		} else if existing != nil && existing.ID != excludeID {
			return user.ErrEmailConflict
		}
	}
	if u.PhoneNumber != "" {
		if existing, err := repo.FindByPhoneNumber(ctx, u.PhoneNumber); err != nil {
			if !errors.Is(err, user.ErrUserNotFound) {
				return err
			}
		} else if existing != nil && existing.ID != excludeID {
			return user.ErrPhoneConflict
		}
	}
	return nil
}
