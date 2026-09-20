// Package bizpkg holds cross-module application helpers shared by the
// application services: stateless, derived only from the request context,
// never a capability call — module-to-module capabilities go through
// crossdomain contracts (ADR 0005). The charter is recorded in ADR 0010.
package bizpkg

import (
	"context"

	authnservice "glex/internal/domain/authn/service"
)

// AuditActor returns the System User id stamped into a write's audit fields
// (ADR 0002): the authenticated Principal when one is present, 0 when the
// system itself acts (bootstrap, migration). Client DTOs never carry the
// audit fields; this is the only writer.
func AuditActor(ctx context.Context) int64 {
	if principal := authnservice.PrincipalFromContext(ctx); principal != nil {
		return principal.SystemUserID()
	}
	return 0
}
