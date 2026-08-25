package access

import (
	"context"
	"sort"

	"glex/internal/crossdomain/authn"
	authnmodel "glex/internal/crossdomain/authn/model"
)

// revokeAllSystemUsers revokes every Login Session of the given System Users
// — the one revocation policy access's security-changing writes classify
// (Role, Menu Grant, and Role Assignment writes are always administrative).
// Ids are validated, deduplicated, and sorted, then sent as one flat request
// through the authn crossdomain contract at the caller-selected point in its
// Application transaction. The user module's richer self-service policy
// vocabulary is deliberately not access's.
func revokeAllSystemUsers(ctx context.Context, revoker authn.SessionRevoker, systemUserIDs ...int64) error {
	byUser := make(map[int64]struct{}, len(systemUserIDs))
	for _, id := range systemUserIDs {
		if id > 0 {
			byUser[id] = struct{}{}
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
	return revoker.RevokeSystemUsers(ctx, &authnmodel.RevocationRequest{
		SystemUserIDs: ids,
	})
}
