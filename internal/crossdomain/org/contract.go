// Package org publishes the org module's cross-module contracts in the
// anti-corruption shape of ADR 0005: contract.go holds the consumer-shaped
// interface, impl/ adapts the provider's domain capability, and orgmock
// holds the mockgen-generated test double. There is deliberately no model/
// package — both capabilities pass scalars only. Contracts are consumed by
// constructor injection into the calling application service; there is
// deliberately no package-level service locator, and cmd stays the only
// composition root.
package org

import (
	"context"
)

//go:generate go tool mockgen -package orgmock -destination orgmock/dept_reader.go glex/internal/crossdomain/org DeptReader

// DeptReader exposes the org module's department reads to the user module
// (ADR 0013). Errors cross this seam as the org domain's kratos errors with
// their org reasons (DEPT_NOT_FOUND, DEPT_DISABLED) — consumers surface them
// verbatim, the wire shape is stable.
type DeptReader interface {
	// EnsureActiveDept rejects departments that are missing or disabled —
	// the user-save validation behind a user's department reference.
	EnsureActiveDept(ctx context.Context, deptID int64) error
	// ResolveSubtreeIDs returns the department's id plus every descendant
	// id — the department-subtree filter behind the user list.
	ResolveSubtreeIDs(ctx context.Context, rootDeptID int64) ([]int64, error)
	// DeptPaths maps department ids to their root-first display paths
	// ("总公司/研发部") — Data Export's department column (ADR 0017).
	// Unknown or withdrawn ids are absent from the map; the export cell
	// degrades to empty, one sour reference never fails the page.
	DeptPaths(ctx context.Context, deptIDs []int64) (map[int64]string, error)
	// ResolveDeptPath maps a root-first display path back to the
	// department id — Data Import's department column (ADR 0017).
	// DEPT_NOT_FOUND for an unknown path, DEPT_DISABLED for a disabled
	// department; the org wire shape consumers surface.
	ResolveDeptPath(ctx context.Context, path string) (int64, error)
}
