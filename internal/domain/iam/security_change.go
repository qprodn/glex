package iam

import "context"

// SessionRevocation is the explicit domain policy for a security-changing
// write. Its fields are private so callers cannot encode policy with an empty
// Session ID or an arbitrary primitive value.
type SessionRevocation struct {
	required           bool
	preservedSessionID string
}

// RevokeAllLoginSessions is the default policy for administrative security
// changes.
func RevokeAllLoginSessions() SessionRevocation {
	return SessionRevocation{required: true}
}

// PreserveLoginSession revokes every Login Session except sessionID. An empty
// ID safely degrades to all-Session revocation.
func PreserveLoginSession(sessionID string) SessionRevocation {
	return SessionRevocation{required: true, preservedSessionID: sessionID}
}

// Required reports whether a Redis revocation is part of this change.
func (r SessionRevocation) Required() bool { return r.required }

// PreservedSessionID returns the one Session excluded from revocation, if any.
func (r SessionRevocation) PreservedSessionID() string { return r.preservedSessionID }

// SystemUserSessionRevocation is one affected System User in an atomic Redis
// revocation batch.
type SystemUserSessionRevocation struct {
	SystemUserID int64
	Policy       SessionRevocation
}

// AuthorizationChangedFrom classifies Role writes that invalidate snapshots.
func (r *Role) AuthorizationChangedFrom(current *Role) bool {
	return r != nil && current != nil && (r.RoleKey != current.RoleKey || r.Status != current.Status)
}

// AuthorizationChangedFrom classifies Menu writes that invalidate snapshots.
func (m *Menu) AuthorizationChangedFrom(current *Menu) bool {
	return m != nil && current != nil && (m.Perms != current.Perms || m.Status != current.Status)
}

// SessionRevocationFrom classifies System User writes. Status changes always
// revoke all Sessions. Password writes use the caller's explicit self/admin
// policy; other profile writes do not affect authorization.
func (u *User) SessionRevocationFrom(current *User, passwordPolicy SessionRevocation) SessionRevocation {
	if u == nil || current == nil {
		return SessionRevocation{}
	}
	if u.Status != current.Status {
		return RevokeAllLoginSessions()
	}
	if u.Password != "" {
		return passwordPolicy
	}
	return SessionRevocation{}
}

// SessionRevoker is the slice of LoginSessionRepo that a security-changing
// write pairs with its database mutation. Implemented by the Login Session
// repository.
type SessionRevoker interface {
	RevokeSystemUsers(ctx context.Context, revocations []SystemUserSessionRevocation) error
}
