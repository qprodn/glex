package impl

import (
	"context"
	"errors"
	"testing"

	"glex/internal/crossdomain/org"
	orgdomain "glex/internal/domain/org"
	orgservice "glex/internal/domain/org/service"
)

// recordingDeptService records the two contract asks; the embedded interface
// stays nil because the adapter under test touches nothing else.
type recordingDeptService struct {
	orgservice.Dept
	ensureID   int64
	ensureErr  error
	subtreeID  int64
	subtree    []int64
	subtreeErr error
}

func (s *recordingDeptService) EnsureActive(_ context.Context, deptID int64) error {
	s.ensureID = deptID
	return s.ensureErr
}

func (s *recordingDeptService) SubtreeIDs(_ context.Context, rootDeptID int64) ([]int64, error) {
	s.subtreeID = rootDeptID
	return s.subtree, s.subtreeErr
}

func TestDeptReaderDelegatesEnsureActive(t *testing.T) {
	depts := &recordingDeptService{ensureErr: orgdomain.ErrDeptDisabled}
	reader := NewDeptReader(depts)

	if err := reader.EnsureActiveDept(t.Context(), 42); !errors.Is(err, orgdomain.ErrDeptDisabled) {
		t.Fatalf("err = %v, want org's ErrDeptDisabled verbatim", err)
	}
	if depts.ensureID != 42 {
		t.Fatalf("ensureID = %d, want 42", depts.ensureID)
	}
}

func TestDeptReaderDelegatesSubtreeIDs(t *testing.T) {
	depts := &recordingDeptService{subtree: []int64{1, 2}}
	reader := NewDeptReader(depts)

	ids, err := reader.ResolveSubtreeIDs(t.Context(), 1)
	if err != nil {
		t.Fatalf("ResolveSubtreeIDs: %v", err)
	}
	if len(ids) != 2 || depts.subtreeID != 1 {
		t.Fatalf("ids = %v, root = %d, want [1 2] root 1", ids, depts.subtreeID)
	}
}

// The contract package is referenced by the adapter's return type.
var _ org.DeptReader = (*deptReader)(nil)
