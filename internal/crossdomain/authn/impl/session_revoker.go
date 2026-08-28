// Package impl adapts authn's domain capabilities to the crossdomain
// contracts. Adapters live here rather than in infra (ADR 0005): this is the
// only place that knows both the contract snapshots and the provider's domain
// services.
package impl

import (
	"context"

	"glex/internal/crossdomain/authn"
	"glex/internal/crossdomain/authn/model"
	authnservice "glex/internal/domain/authn/service"
)

// sessionRevoker satisfies the revocation contract by delegating to the
// provider's Online Session domain service.
type sessionRevoker struct {
	sessions authnservice.OnlineSession
}

// NewSessionRevoker adapts the authn Online Session domain service to the
// crossdomain revocation contract.
func NewSessionRevoker(sessions authnservice.OnlineSession) authn.SessionRevoker {
	return &sessionRevoker{sessions: sessions}
}

// RevokeSystemUsers translates the flat request into the provider's batch
// revocation. An empty preserved session degrades to full revocation, which is
// the provider's own policy semantic for that value.
func (s *sessionRevoker) RevokeSystemUsers(ctx context.Context, request *model.RevocationRequest) error {
	if request == nil || len(request.SystemUserIDs) == 0 {
		return nil
	}
	return s.sessions.RevokeSystemUsers(ctx, request.SystemUserIDs, request.PreservedSessionID)
}
