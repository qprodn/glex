package impl

import (
	"context"

	"glex/internal/crossdomain/user"
	userservice "glex/internal/domain/user/service"
)

// deptUsageReader satisfies the user department-usage contract by delegating
// to the provider's System User domain service: the active-user reference
// count lives there, beside the user module's domain policy.
type deptUsageReader struct {
	users userservice.User
}

// NewDeptUsageReader adapts the user domain service to the crossdomain
// department-usage contract.
func NewDeptUsageReader(users userservice.User) user.DeptUsageReader {
	return &deptUsageReader{users: users}
}

// HasUsersInDept reports whether any active System User's department
// reference points at deptID.
func (r *deptUsageReader) HasUsersInDept(ctx context.Context, deptID int64) (bool, error) {
	return r.users.HasUsersInDept(ctx, deptID)
}
