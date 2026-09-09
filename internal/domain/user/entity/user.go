// Package entity holds the System User module's domain objects and value
// objects. Pure domain models: no proto, no storage tags.
package entity

import (
	"time"
)

// User is a system-user domain object (系统用户), ported from RuoYi sys_user.
type User struct {
	ID          int64
	UserName    string
	NickName    string
	Email       string
	PhoneNumber string
	Gender      int32
	// Password carries the caller's requested plaintext credential on
	// create/update requests only. It is never populated on reads and never
	// persisted by this module: the stored hash belongs to authn and is
	// written solely through its credential capability (ADR 0005).
	Password string
	Status   int32
	// Avatar is the user's avatar as a Stored File id (0 = none) — RuoYi's
	// bigint avatar column. The file module resolves it to a short-TTL
	// Display URL on reads through the crossdomain file contract; write
	// paths do not validate it — an unresolvable id degrades to the default
	// avatar (ADR 0016).
	Avatar int64
	Remark string
	// DeptID is the user's place in the org structure (0 = none). The org
	// module validates it through the crossdomain org contract; the user
	// module stores it (ADR 0013).
	DeptID    int64
	CreateBy  int64
	UpdateBy  int64
	LoginIP   string
	LoginDate *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserUpdate pairs the persisted System User with its pre-image so the
// Application service can classify session revocation.
type UserUpdate struct {
	User     *User
	Previous *User
}

// SessionRevocationFrom classifies System User writes. The receiver is the
// requested change. Status changes always revoke all Sessions. Password
// requests (a non-empty request Password, delegated to authn's credential
// capability) use the caller's explicit self/admin policy; other profile
// writes do not affect authorization.
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

// CurrentUserInfo combines the current System User record with the immutable
// Role/Functional-Permission view authenticated into the request Principal.
// Password remains on User only for credential/update workflows and is never
// projected by the service's dedicated current-user DTO conversion.
type CurrentUserInfo struct {
	User        *User
	RoleKeys    []string
	Permissions []string
}

// SessionRevocation is the explicit policy for a security-changing write in
// this module: whether Login Sessions must be revoked, and the one Session a
// self-service change keeps. Its fields are private so callers cannot encode
// policy with an empty Session ID or an arbitrary primitive value. The
// revocation itself is performed through the authn crossdomain contract; this
// type never crosses the module seam (ADR 0005).
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

// SystemUserSessionRevocation pairs one affected System User with its policy;
// the Application service folds a batch of these into the flat crossdomain
// request.
type SystemUserSessionRevocation struct {
	SystemUserID int64
	Policy       SessionRevocation
}
