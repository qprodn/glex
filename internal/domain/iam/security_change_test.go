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

func TestMenuAuthorizationChangeTriggers(t *testing.T) {
	current := &Menu{Perms: "system:user:list", Status: 0, MenuName: "Users"}
	for _, tc := range []struct {
		name string
		next *Menu
		want bool
	}{
		{"perms", &Menu{Perms: "system:user:edit", Status: 0, MenuName: "Users"}, true},
		{"status", &Menu{Perms: "system:user:list", Status: 1, MenuName: "Users"}, true},
		{"non-security field", &Menu{Perms: "system:user:list", Status: 0, MenuName: "People"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.next.AuthorizationChangedFrom(current); got != tc.want {
				t.Fatalf("AuthorizationChangedFrom = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSystemUserSecurityChangeTriggersAndPasswordPolicy(t *testing.T) {
	current := &User{Status: 0, UserName: "alice"}
	self := PreserveLoginSession("current-session")

	if got := (&User{Status: 0, UserName: "renamed"}).SessionRevocationFrom(current, self); got.Required() {
		t.Fatalf("profile-only update requires revocation: %#v", got)
	}
	if got := (&User{Status: 0, Password: "new-hash"}).SessionRevocationFrom(current, self); !got.Required() || got.PreservedSessionID() != "current-session" {
		t.Fatalf("self password policy = %#v", got)
	}
	if got := (&User{Status: 0, Password: "new-hash"}).SessionRevocationFrom(current, RevokeAllLoginSessions()); !got.Required() || got.PreservedSessionID() != "" {
		t.Fatalf("administrative reset policy = %#v", got)
	}
	if got := (&User{Status: 1, Password: "new-hash"}).SessionRevocationFrom(current, self); !got.Required() || got.PreservedSessionID() != "" {
		t.Fatalf("status change did not override self preservation: %#v", got)
	}
}
