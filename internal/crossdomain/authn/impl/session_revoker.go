// Package impl adapts authn's domain capabilities to the crossdomain
// contracts. Adapters live here rather than in infra (ADR 0017): this is the
// only place that knows both the contract snapshots and the provider's domain
// types.
package impl

import (
	"context"

	"glex/internal/crossdomain/authn"
	"glex/internal/crossdomain/authn/model"
	iam "glex/internal/domain/iam"
)

// sessionRevoker satisfies the revocation contract by wrapping the Login
// Session repository.
type sessionRevoker struct {
	sessions iam.LoginSessionRepo
}

// NewSessionRevoker adapts the Login Session repository to the crossdomain
// revocation contract.
func NewSessionRevoker(sessions iam.LoginSessionRepo) authn.SessionRevoker {
	return &sessionRevoker{sessions: sessions}
}

// RevokeSystemUsers translates the flat request into the provider's batch
// revocation. An empty preserved session degrades to full revocation, which is
// the provider's own policy semantic for that value.
func (s *sessionRevoker) RevokeSystemUsers(ctx context.Context, request *model.RevocationRequest) error {
	if request == nil || len(request.SystemUserIDs) == 0 {
		return nil
	}
	batch := make([]iam.SystemUserSessionRevocation, 0, len(request.SystemUserIDs))
	for _, id := range request.SystemUserIDs {
		batch = append(batch, iam.SystemUserSessionRevocation{
			SystemUserID: id,
			Policy:       iam.PreserveLoginSession(request.PreservedSessionID),
		})
	}
	return s.sessions.RevokeSystemUsers(ctx, batch)
}
