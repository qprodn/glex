package iam

import "testing"

func TestRoleAuthorizationChangeTriggers(t *testing.T) {
	current := &Role{RoleKey: "viewer", Status: 0, RoleName: "Viewer"}
	for _, tc := range []struct {
		name string
		next *Role
		want bool
	}{
		{"role_key", &Role{RoleKey: "operator", Status: 0, RoleName: "Viewer"}, true},
		{"status", &Role{RoleKey: "viewer", Status: 1, RoleName: "Viewer"}, true},
		{"non-security field", &Role{RoleKey: "viewer", Status: 0, RoleName: "Renamed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.next.AuthorizationChangedFrom(current); got != tc.want {
				t.Fatalf("AuthorizationChangedFrom = %v, want %v", got, tc.want)
			}
		})
	}
}
