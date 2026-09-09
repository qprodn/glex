package impl

import (
	"context"

	"glex/internal/crossdomain/org"
	orgservice "glex/internal/domain/org/service"
)

// deptReader satisfies the org department-read contract by delegating to the
// provider's department domain service: the guards and the subtree
// resolution live there, beside the org module's domain policy.
type deptReader struct {
	depts orgservice.Dept
}

// NewDeptReader adapts the org domain service to the crossdomain
// department-read contract.
func NewDeptReader(depts orgservice.Dept) org.DeptReader {
	return &deptReader{depts: depts}
}

// EnsureActiveDept passes the org domain's validation through: missing and
// disabled departments return org's kratos errors (DEPT_NOT_FOUND,
// DEPT_DISABLED) verbatim — the wire shape is the contract.
func (r *deptReader) EnsureActiveDept(ctx context.Context, deptID int64) error {
	return r.depts.EnsureActive(ctx, deptID)
}

// ResolveSubtreeIDs returns the department's id plus every descendant id —
// disabled departments included (disabling hides a department from selects,
// not from its users).
func (r *deptReader) ResolveSubtreeIDs(ctx context.Context, rootDeptID int64) ([]int64, error) {
	return r.depts.SubtreeIDs(ctx, rootDeptID)
}

// DeptPaths delegates to the domain service's batch read; unknown ids stay
// absent so callers degrade per id (ADR 0017).
func (r *deptReader) DeptPaths(ctx context.Context, deptIDs []int64) (map[int64]string, error) {
	return r.depts.DeptPaths(ctx, deptIDs)
}

// ResolveDeptPath passes the org domain's path resolution through: unknown
// paths and disabled departments return org's kratos errors
// (DEPT_NOT_FOUND, DEPT_DISABLED) verbatim — the wire shape is the
// contract's.
func (r *deptReader) ResolveDeptPath(ctx context.Context, path string) (int64, error) {
	return r.depts.ResolveDeptPath(ctx, path)
}
