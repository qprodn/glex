package entity

import (
	"strings"
	"testing"
)

func TestPrincipalIsImmutableAndContainsNoBearerCredential(t *testing.T) {
	roles := []string{"operator"}
	permissions := []string{"system:user:list"}
	principal := NewPrincipal(42, "alice", "session-id", roles, permissions)
	roles[0] = "changed"
	permissions[0] = "changed"

	gotRoles := principal.RoleKeys()
	gotPermissions := principal.Permissions()
	gotRoles[0] = "changed-again"
	gotPermissions[0] = "changed-again"
	if principal.RoleKeys()[0] != "operator" || principal.Permissions()[0] != "system:user:list" {
		t.Fatal("Principal collections were mutated through caller-owned slices")
	}
	if principal.SystemUserID() != 42 || principal.UserName() != "alice" || principal.SessionID() != "session-id" {
		t.Fatalf("Principal identity = %d/%q/%q", principal.SystemUserID(), principal.UserName(), principal.SessionID())
	}
	if strings.Contains(strings.ToLower(strings.Join(append(principal.RoleKeys(), principal.Permissions()...), ",")), "bearer") {
		t.Fatal("Principal unexpectedly contains bearer data")
	}
}

func TestPrincipalPermissionChecks(t *testing.T) {
	tests := []struct {
		name        string
		permissions []string
		required    string
		want        bool
	}{
		{name: "exact", permissions: []string{"system:user:list"}, required: "system:user:list", want: true},
		{name: "missing", permissions: []string{"system:user:list"}, required: "system:user:edit"},
		{name: "superadmin", permissions: []string{"*:*:*"}, required: "system:user:edit", want: true},
		{name: "partial wildcard is not special", permissions: []string{"system:user:*"}, required: "system:user:edit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principal := NewPrincipal(42, "alice", "session-id", nil, tt.permissions)
			if got := principal.HasPermission(tt.required); got != tt.want {
				t.Fatalf("HasPermission(%q) = %v, want %v", tt.required, got, tt.want)
			}
		})
	}
}
