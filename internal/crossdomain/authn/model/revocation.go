// Package model holds the flat snapshots that cross the authn crossdomain
// contract seam. Types here are consumer-shaped scalars only: the provider's
// domain objects never appear, so consumers never import the provider's
// domain package (ADR 0017).
package model

// RevocationRequest revokes the Login Sessions of the affected System Users.
// PreservedSessionID optionally spares exactly one Login Session belonging to
// one of them; empty spares none.
type RevocationRequest struct {
	SystemUserIDs      []int64
	PreservedSessionID string
}
