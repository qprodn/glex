// Package model holds the flat snapshots that cross the access crossdomain
// contract seam. Types here are consumer-shaped scalars only: the provider's
// domain objects never appear, so consumers never import the provider's
// domain package (ADR 0005).
package model

// AuthorizationResolution is the resolved authorization content of one System
// User: the active Role Keys and the Functional Permissions derived from the
// user's Role Assignments and Menu Grants. The carrier fields of an
// Authorization Snapshot (identity) stay with the consumer.
type AuthorizationResolution struct {
	RoleKeys    []string
	Permissions []string
}
