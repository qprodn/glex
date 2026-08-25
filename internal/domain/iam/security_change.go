package iam

// SessionRevocation is the explicit policy for a security-changing write in
// this module: whether Login Sessions must be revoked, and the one Session a
// self-service change keeps. Its fields are private so callers cannot encode
// policy with an empty Session ID or an arbitrary primitive value. The
// revocation itself is performed through the authn crossdomain contract; this
// type never crosses the module seam (ADR 0016).
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
// the caller Usecase folds a batch of these into the flat crossdomain request.
type SystemUserSessionRevocation struct {
	SystemUserID int64
	Policy       SessionRevocation
}
