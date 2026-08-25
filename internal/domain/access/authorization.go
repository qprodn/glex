package access

import (
	"context"

	"github.com/go-kratos/kratos/v3/errors"
)

// SuperadminRoleKey is the Role key whose resolution yields the wildcard
// Functional Permission `*:*:*` (CONTEXT.md "Superadmin"). Which Role key that
// is belongs to access; matching the wildcard is the carrier's only job.
const SuperadminRoleKey = "superadmin"

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
// Authorization Snapshot. Its only consumer is authn's login Usecase, reached
// through the crossdomain access contract (ADR 0017) — never by importing
// this package.
type AuthorizationResolver interface {
	// ResolveAuthorization freshly reads the System User's active Role
	// Assignments and Menu Grants and computes the resolution. Reads join the
	// transaction propagated through ctx when one is active.
	ResolveAuthorization(ctx context.Context, systemUserID int64) (*AuthorizationResolution, error)
}

var (
	// ErrAuthorizationUnavailable: resolution could not read its backing
	// store. The reason is internal to access; consumers map it onto their
	// own availability vocabulary, so it never reaches a client as-is.
	ErrAuthorizationUnavailable = errors.New(500, "AUTHORIZATION_UNAVAILABLE", "authorization resolution unavailable")
)
