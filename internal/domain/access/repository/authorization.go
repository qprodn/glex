package repository

import (
	"context"
)

// AuthorizationResolution is access's computed answer to "what is this System
// User allowed?": the active Role Keys and the distinct Functional Permissions
// derived from the user's active Role Assignments and their Menu Grants
// (CONTEXT.md "Authorization Resolution"). Which Role key is Superadmin and
// therefore yields the wildcard `*:*:*` permission is decided here, by
// resolution; matching the wildcard afterwards is the carrier's only job.
type AuthorizationResolution struct {
	RoleKeys    []string
	Permissions []string
}

// AuthorizationResolver resolves the authorization content captured by an
// Authorization Snapshot. Its only consumer is authn's login Application
// service, reached through the crossdomain access contract (ADR 0017) — never
// by importing this package.
type AuthorizationResolver interface {
	// ResolveAuthorization freshly reads the System User's active Role
	// Assignments and Menu Grants and computes the resolution. Reads join the
	// transaction propagated through ctx when one is active.
	ResolveAuthorization(ctx context.Context, systemUserID int64) (*AuthorizationResolution, error)
}
