package accessinfra

import (
	"strings"
	"testing"

	"glex/internal/infra/ent"
)

func TestResolutionAddsWildcardOnlyThroughSuperadminRole(t *testing.T) {
	resolution := toBizResolution([]*ent.Role{
		{ID: 1, RoleKey: "viewer"},
		{ID: 2, RoleKey: "superadmin"},
	}, []*ent.Menu{
		{ID: 10, Perms: "system:user:list"},
		{ID: 11, Perms: " system:user:list "},
		{ID: 12, Perms: ""},
	})
	if strings.Join(resolution.RoleKeys, ",") != "superadmin,viewer" {
		t.Fatalf("Role keys = %v", resolution.RoleKeys)
	}
	if strings.Join(resolution.Permissions, ",") != "*:*:*,system:user:list" {
		t.Fatalf("Functional Permissions = %v", resolution.Permissions)
	}

	withoutWildcard := toBizResolution([]*ent.Role{{ID: 1, RoleKey: "viewer"}}, []*ent.Menu{{ID: 13, Perms: "*:*:*"}})
	if len(withoutWildcard.Permissions) != 0 {
		t.Fatalf("wildcard added without superadmin Role: %v", withoutWildcard.Permissions)
	}
	malformedSuperadmin := toBizResolution([]*ent.Role{{ID: 2, RoleKey: " superadmin "}}, nil)
	if len(malformedSuperadmin.Permissions) != 0 {
		t.Fatalf("non-exact superadmin Role key granted wildcard: %v", malformedSuperadmin.Permissions)
	}
}
