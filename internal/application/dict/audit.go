package dict

import (
	"context"

	authnservice "glex/internal/domain/authn/service"
)

// auditActor returns the System User id stamped into a write's audit fields
// (ADR 0002): the authenticated Principal when one is present, 0 when the
// system itself acts (bootstrap, migration). Client DTOs never carry the
// audit fields; this is the only writer.
func auditActor(ctx context.Context) int64 {
	if principal, ok := authnservice.PrincipalFromContext(ctx); ok {
		return principal.SystemUserID()
	}
	return 0
}
