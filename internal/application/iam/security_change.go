package iam

import (
	"context"
	"sort"

	iamdomain "glex/internal/domain/iam"
)

// revokeSystemUsers batches revocations for the affected System Users —
// filtered to required policies, deduplicated per user (last policy wins),
// sorted by id — and performs them at the caller-selected point in its
// Application transaction.
func revokeSystemUsers(ctx context.Context, revoker iamdomain.SessionRevoker, revocations ...iamdomain.SystemUserSessionRevocation) error {
	byUser := make(map[int64]iamdomain.SessionRevocation, len(revocations))
	for _, revocation := range revocations {
		if revocation.SystemUserID > 0 && revocation.Policy.Required() {
			byUser[revocation.SystemUserID] = revocation.Policy
		}
	}
	if len(byUser) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(byUser))
	for id := range byUser {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	batch := make([]iamdomain.SystemUserSessionRevocation, 0, len(ids))
	for _, id := range ids {
		batch = append(batch, iamdomain.SystemUserSessionRevocation{SystemUserID: id, Policy: byUser[id]})
	}
	return revoker.RevokeSystemUsers(ctx, batch)
}

// revokeAllSystemUsers revokes every Login Session of the given System Users
// (the default administrative policy).
func revokeAllSystemUsers(ctx context.Context, revoker iamdomain.SessionRevoker, systemUserIDs ...int64) error {
	revocations := make([]iamdomain.SystemUserSessionRevocation, 0, len(systemUserIDs))
	for _, id := range systemUserIDs {
		revocations = append(revocations, iamdomain.SystemUserSessionRevocation{SystemUserID: id, Policy: iamdomain.RevokeAllLoginSessions()})
	}
	return revokeSystemUsers(ctx, revoker, revocations...)
}
