package user

import "testing"

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
