// Package user publishes the user module's cross-module contracts in the
// anti-corruption shape of ADR 0005: contract.go holds the consumer-shaped
// interface, impl/ adapts the provider's domain capability, and usermock
// holds the mockgen-generated test double. There is deliberately no model/
// package — the capability passes scalars only. Contracts are consumed by
// constructor injection into the calling application service; there is
// deliberately no package-level service locator, and cmd stays the only
// composition root.
package user

import (
	"context"
)

//go:generate go tool mockgen -package usermock -destination usermock/dept_usage_reader.go glex/internal/crossdomain/user DeptUsageReader

// DeptUsageReader exposes the user module's department-reference reads to
// the org module (ADR 0013): the guards behind "a department with users
// cannot be deleted or disabled".
type DeptUsageReader interface {
	// HasUsersInDept reports whether any active System User's department
	// reference points at deptID.
	HasUsersInDept(ctx context.Context, deptID int64) (bool, error)
}
