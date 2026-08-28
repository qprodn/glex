package entity

// SuperadminRoleKey is the Role key whose resolution yields the wildcard
// Functional Permission `*:*:*` (CONTEXT.md "Superadmin"). Which Role key that
// is belongs to access; matching the wildcard afterwards is the carrier's only
// job.
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
