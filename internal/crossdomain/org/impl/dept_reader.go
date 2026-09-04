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
