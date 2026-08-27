package service

import (
	"context"

	"glex/internal/domain/access"
	"glex/internal/domain/access/repository"
)

// AssertUserExists gates the by-user endpoints on the System User existing
// (and not being soft-deleted) — access's own copy of the reason, answered by
// the assignment storage's user edge.
func AssertUserExists(ctx context.Context, linkRepo repository.UserRoleRepo, userID int64) error {
	exists, err := linkRepo.UserExists(ctx, userID)
	if err != nil {
		return err
	}
	if !exists {
		return access.ErrUserNotFound
	}
	return nil
}

// AssertRolesAssignable dedupes roleIDs and checks every id exists and is not
// soft-deleted. The active-role set is the freshly-read assignment list — the
// same data the response carries, so no second query (Menu Grant instead
// re-reads via ListByIDs; here the read is needed anyway for the reply). An
// empty list is legal — it means "no roles" — and passes without touching
// anything.
func AssertRolesAssignable(assignments []*repository.RoleAssignment, roleIDs []int64) ([]int64, error) {
	unique := dedupeIDs(roleIDs)
	if len(unique) == 0 {
		return unique, nil
	}
	byID := make(map[int64]struct{}, len(assignments))
	for _, a := range assignments {
		byID[a.Role.ID] = struct{}{}
	}
	for _, id := range unique {
		if _, ok := byID[id]; !ok {
			return nil, access.ErrRoleNotFound
		}
	}
	return unique, nil
}
