package impl

import (
	"context"
	"testing"

	userservice "glex/internal/domain/user/service"

	"glex/internal/crossdomain/user"
)

// recordingUserService records the usage ask; the embedded interface stays
// nil because the adapter under test touches nothing else.
type recordingUserService struct {
	userservice.User
	askedDept int64
	has       bool
	err       error
}

func (s *recordingUserService) HasUsersInDept(_ context.Context, deptID int64) (bool, error) {
	s.askedDept = deptID
	return s.has, s.err
}

func TestDeptUsageReaderDelegates(t *testing.T) {
	users := &recordingUserService{has: true}
	reader := NewDeptUsageReader(users)

	has, err := reader.HasUsersInDept(t.Context(), 3)
	if err != nil || !has {
		t.Fatalf("has/err = %v/%v, want true/nil", has, err)
	}
	if users.askedDept != 3 {
		t.Fatalf("askedDept = %d, want 3", users.askedDept)
	}
}

// The contract package is referenced by the adapter's return type.
var _ user.DeptUsageReader = (*deptUsageReader)(nil)
