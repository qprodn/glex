package entity

import (
	"time"
)

// LoginSession is one revocable sign-in by a System User. ID is the public,
// non-secret Session ID; the bearer token deliberately is not part of this DO.
type LoginSession struct {
	ID           string
	SystemUserID int64
	UserName     string
	DeviceType   string
	IPAddress    string
	Browser      string
	OS           string
	LoginTime    time.Time
}

// AuthorizationSnapshot is the shared account-level view used by every Login
// Session of one System User. It contains active Role keys and distinct
// Functional Permission handles only; route-tree presentation is deliberately
// not part of this snapshot.
type AuthorizationSnapshot struct {
	SystemUserID int64
	UserName     string
	RoleKeys     []string
	Permissions  []string
}

// LoginResult returns the bearer credential exactly once after the shared
// Authorization Snapshot and authoritative Login Session both exist.
type LoginResult struct {
	AccessToken string
	ExpiresIn   time.Duration
}
