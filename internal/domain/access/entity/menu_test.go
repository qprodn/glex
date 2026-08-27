package entity

import "testing"

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
