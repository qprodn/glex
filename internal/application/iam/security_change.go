package iam

import (
	"context"
	"fmt"
	"sort"

	"glex/internal/crossdomain/authn"
	authnmodel "glex/internal/crossdomain/authn/model"
	iamdomain "glex/internal/domain/iam"
)

// revokeSystemUsers assembles the flat revocation request for the affected
// System Users — filtered to required policies, deduplicated per user (last
// policy wins), sorted by id — and performs it at the caller-selected point in
// its Application transaction. The flat request carries at most one preserved
// Login Session: the only producer of a preserved policy is a single-user self
// password change, and conflicting preserves are rejected rather than
// silently resolved.
func revokeSystemUsers(ctx context.Context, revoker authn.SessionRevoker, revocations ...iamdomain.SystemUserSessionRevocation) error {
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
	preservedSessionID := ""
	for _, id := range ids {
		preserved := byUser[id].PreservedSessionID()
		if preserved == "" {
			continue
		}
		if preservedSessionID != "" && preservedSessionID != preserved {
			return fmt.Errorf("revoke system users: batch preserves conflicting sessions %q and %q", preservedSessionID, preserved)
		}
		preservedSessionID = preserved
	}
	return revoker.RevokeSystemUsers(ctx, &authnmodel.RevocationRequest{
		SystemUserIDs:      ids,
		PreservedSessionID: preservedSessionID,
	})
}

// revokeAllSystemUsers revokes every Login Session of the given System Users
// (the default administrative policy).
func revokeAllSystemUsers(ctx context.Context, revoker authn.SessionRevoker, systemUserIDs ...int64) error {
	revocations := make([]iamdomain.SystemUserSessionRevocation, 0, len(systemUserIDs))
	for _, id := range systemUserIDs {
		revocations = append(revocations, iamdomain.SystemUserSessionRevocation{SystemUserID: id, Policy: iamdomain.RevokeAllLoginSessions()})
	}
	return revokeSystemUsers(ctx, revoker, revocations...)
}
