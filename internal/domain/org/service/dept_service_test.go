package service

import (
	"context"
	"errors"
	"testing"

	"glex/internal/domain/org"
	"glex/internal/domain/org/entity"
	"glex/internal/domain/org/repository"
)

// fakeDeptRepo is an in-memory repository.DeptRepo for testing the dept
// domain service in isolation from the infrastructure layer. rows holds every
// non-withdrawn department keyed by id; the recorded slices capture the
// writes the service issued (enable cascades, deletes).
type fakeDeptRepo struct {
	rows       map[int64]*entity.Dept
	nextID     int64
	enabledIDs [][]int64
	deleted    []int64
	created    []*entity.Dept
	updated    []*entity.Dept
	listErr    error
}

func newFakeDeptRepo(rows ...*entity.Dept) *fakeDeptRepo {
	r := &fakeDeptRepo{rows: make(map[int64]*entity.Dept, len(rows)), nextID: 100}
	for _, d := range rows {
		r.rows[d.ID] = d
	}
	return r
}

func (r *fakeDeptRepo) FindByID(_ context.Context, id int64) (*entity.Dept, error) {
	d, ok := r.rows[id]
	if !ok {
		return nil, org.ErrDeptNotFound
	}
	return d, nil
}

func (r *fakeDeptRepo) FindByParentAndName(_ context.Context, parentID int64, name string) (*entity.Dept, error) {
	for _, d := range r.rows {
		if d.ParentID == parentID && d.DeptName == name {
			return d, nil
		}
	}
	return nil, org.ErrDeptNotFound
}

func (r *fakeDeptRepo) ListDepts(context.Context) ([]*entity.Dept, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]*entity.Dept, 0, len(r.rows))
	for _, d := range r.rows {
		out = append(out, d)
	}
	return out, nil
}

func (r *fakeDeptRepo) CreateDept(_ context.Context, d *entity.Dept) (*entity.Dept, error) {
	d.ID = r.nextID
	r.nextID++
	r.rows[d.ID] = d
	r.created = append(r.created, d)
	return d, nil
}

func (r *fakeDeptRepo) UpdateDept(_ context.Context, d *entity.Dept) (*entity.Dept, *entity.Dept, error) {
	previous, ok := r.rows[d.ID]
	if !ok {
		return nil, nil, org.ErrDeptNotFound
	}
	r.rows[d.ID] = d
	r.updated = append(r.updated, d)
	return d, previous, nil
}

func (r *fakeDeptRepo) EnableDepts(_ context.Context, ids []int64, _ int64) error {
	r.enabledIDs = append(r.enabledIDs, append([]int64(nil), ids...))
	return nil
}

func (r *fakeDeptRepo) DeleteDept(_ context.Context, id int64, _ int64) error {
	if _, ok := r.rows[id]; !ok {
		return org.ErrDeptNotFound
	}
	delete(r.rows, id)
	r.deleted = append(r.deleted, id)
	return nil
}

func (r *fakeDeptRepo) CountChildDepts(_ context.Context, parentID int64) (int, error) {
	n := 0
	for _, d := range r.rows {
		if d.ParentID == parentID {
			n++
		}
	}
	return n, nil
}

// errRepoFailed is the propagation sentinel: domain tests only assert that a
// repo error crosses the service untouched.
var errRepoFailed = errors.New("repo failed")

// seedTree builds a three-level forest: root(1) → a(2) → a1(3), a2(4); and
// root(1) → b(5). Status is normal unless overridden per node.
func seedTree() []*entity.Dept {
	return []*entity.Dept{
		{ID: 1, ParentID: 0, DeptName: "root", OrderNum: 1},
		{ID: 2, ParentID: 1, DeptName: "a", OrderNum: 1},
		{ID: 3, ParentID: 2, DeptName: "a1", OrderNum: 1},
		{ID: 4, ParentID: 2, DeptName: "a2", OrderNum: 2},
		{ID: 5, ParentID: 1, DeptName: "b", OrderNum: 2},
	}
}

func newDeptSvc(repo repository.DeptRepo) Dept {
	return NewDeptService(repo, testLogger())
}

func TestCreateDept(t *testing.T) {
	repo := newFakeDeptRepo(seedTree()...)
	svc := newDeptSvc(repo)

	created, err := svc.Create(context.Background(), &entity.Dept{
		ParentID: 2, DeptName: "a3", OrderNum: 3, Status: 0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 || len(repo.created) != 1 {
		t.Fatalf("Create did not persist: %+v", created)
	}
}

func TestCreateDept_BlankNameRejected(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	_, err := svc.Create(context.Background(), &entity.Dept{ParentID: 1, DeptName: ""})
	if !errors.Is(err, org.ErrDeptInvalid) {
		t.Fatalf("err = %v, want ErrDeptInvalid", err)
	}
}

func TestCreateDept_MissingParentRejected(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	_, err := svc.Create(context.Background(), &entity.Dept{ParentID: 99, DeptName: "orphan"})
	if !errors.Is(err, org.ErrDeptNotFound) {
		t.Fatalf("err = %v, want ErrDeptNotFound", err)
	}
}

func TestCreateDept_DisabledParentRejected(t *testing.T) {
	rows := seedTree()
	rows[1].Status = 1 // a disabled
	svc := newDeptSvc(newFakeDeptRepo(rows...))
	_, err := svc.Create(context.Background(), &entity.Dept{ParentID: 2, DeptName: "a3"})
	if !errors.Is(err, org.ErrDeptDisabled) {
		t.Fatalf("err = %v, want ErrDeptDisabled", err)
	}
}

func TestCreateDept_SiblingNameTaken(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	_, err := svc.Create(context.Background(), &entity.Dept{ParentID: 2, DeptName: "a1"})
	if !errors.Is(err, org.ErrDeptNameTaken) {
		t.Fatalf("err = %v, want ErrDeptNameTaken", err)
	}
}

func TestCreateDept_RootBypassesParentChecks(t *testing.T) {
	repo := newFakeDeptRepo() // empty store: a parent lookup would fail
	svc := newDeptSvc(repo)
	if _, err := svc.Create(context.Background(), &entity.Dept{ParentID: 0, DeptName: "newroot"}); err != nil {
		t.Fatalf("Create root: %v", err)
	}
}

func TestUpdateDept_MoveUnderDescendantRejected(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	// Moving root(1) under its descendant a1(3) folds the subtree into a cycle.
	_, err := svc.Update(context.Background(), &entity.Dept{ID: 1, ParentID: 3, DeptName: "root"})
	if !errors.Is(err, org.ErrDeptParentInvalid) {
		t.Fatalf("err = %v, want ErrDeptParentInvalid", err)
	}
}

func TestUpdateDept_MoveOntoSelfRejected(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	_, err := svc.Update(context.Background(), &entity.Dept{ID: 2, ParentID: 2, DeptName: "a"})
	if !errors.Is(err, org.ErrDeptParentInvalid) {
		t.Fatalf("err = %v, want ErrDeptParentInvalid", err)
	}
}

func TestUpdateDept_MoveToSiblingParent(t *testing.T) {
	repo := newFakeDeptRepo(seedTree()...)
	svc := newDeptSvc(repo)
	// Moving a1(3) from a(2) under b(5) is a legal reparent.
	if _, err := svc.Update(context.Background(), &entity.Dept{ID: 3, ParentID: 5, DeptName: "a1"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := repo.rows[3].ParentID; got != 5 {
		t.Fatalf("ParentID = %d, want 5", got)
	}
}

func TestUpdateDept_KeepsOwnName(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	if _, err := svc.Update(context.Background(), &entity.Dept{ID: 2, ParentID: 1, DeptName: "a"}); err != nil {
		t.Fatalf("Update keeping own name: %v", err)
	}
}

func TestUpdateDept_SiblingNameTaken(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	_, err := svc.Update(context.Background(), &entity.Dept{ID: 3, ParentID: 2, DeptName: "a2"})
	if !errors.Is(err, org.ErrDeptNameTaken) {
		t.Fatalf("err = %v, want ErrDeptNameTaken", err)
	}
}

func TestUpdateDept_DisableWithEnabledDescendantRejected(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	// Disabling a(2) while a1(3) is enabled is refused — at any depth, the
	// deeper case (disable root under enabled a1) is refused too.
	_, err := svc.Update(context.Background(), &entity.Dept{ID: 2, ParentID: 1, DeptName: "a", Status: 1})
	if !errors.Is(err, org.ErrDeptHasEnabledChildren) {
		t.Fatalf("err = %v, want ErrDeptHasEnabledChildren", err)
	}
}

func TestUpdateDept_DisableAfterDescendantsDisabled(t *testing.T) {
	rows := seedTree()
	rows[2].Status = 1 // a1
	rows[3].Status = 1 // a2
	repo := newFakeDeptRepo(rows...)
	svc := newDeptSvc(repo)
	if _, err := svc.Update(context.Background(), &entity.Dept{ID: 2, ParentID: 1, DeptName: "a", Status: 1}); err != nil {
		t.Fatalf("Update disable: %v", err)
	}
	if repo.rows[2].Status != 1 {
		t.Fatalf("Status = %d, want 1", repo.rows[2].Status)
	}
}

func TestUpdateDept_EnableCascadesAncestors(t *testing.T) {
	rows := seedTree()
	rows[0].Status = 1 // root disabled
	rows[1].Status = 1 // a disabled
	rows[2].Status = 1 // a1 disabled — the update enables it
	repo := newFakeDeptRepo(rows...)
	svc := newDeptSvc(repo)
	if _, err := svc.Update(context.Background(), &entity.Dept{ID: 3, ParentID: 2, DeptName: "a1", Status: 0, UpdateBy: 7}); err != nil {
		t.Fatalf("Update enable: %v", err)
	}
	if len(repo.enabledIDs) != 1 {
		t.Fatalf("EnableDepts called %d times, want 1", len(repo.enabledIDs))
	}
	got := repo.enabledIDs[0]
	if len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("enabled ancestors = %v, want [2 1]", got)
	}
}

func TestUpdateDept_EnableAlreadyEnabledNoCascade(t *testing.T) {
	repo := newFakeDeptRepo(seedTree()...)
	svc := newDeptSvc(repo)
	if _, err := svc.Update(context.Background(), &entity.Dept{ID: 3, ParentID: 2, DeptName: "a1", Status: 0}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(repo.enabledIDs) != 0 {
		t.Fatalf("EnableDepts called %d times, want 0", len(repo.enabledIDs))
	}
}

func TestDeleteDept_RootProtected(t *testing.T) {
	repo := newFakeDeptRepo(&entity.Dept{ID: org.RootDeptID, ParentID: 0, DeptName: "seed root"})
	svc := newDeptSvc(repo)
	err := svc.Delete(context.Background(), org.RootDeptID, 1)
	if !errors.Is(err, org.ErrDeptRootProtected) {
		t.Fatalf("err = %v, want ErrDeptRootProtected", err)
	}
}

func TestDeleteDept_HasChildrenRejected(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	err := svc.Delete(context.Background(), 2, 1)
	if !errors.Is(err, org.ErrDeptHasChildren) {
		t.Fatalf("err = %v, want ErrDeptHasChildren", err)
	}
}

func TestDeleteDept_Leaf(t *testing.T) {
	repo := newFakeDeptRepo(seedTree()...)
	svc := newDeptSvc(repo)
	if err := svc.Delete(context.Background(), 3, 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 3 {
		t.Fatalf("deleted = %v, want [3]", repo.deleted)
	}
}

func TestDeleteDept_Missing(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	err := svc.Delete(context.Background(), 99, 1)
	if !errors.Is(err, org.ErrDeptNotFound) {
		t.Fatalf("err = %v, want ErrDeptNotFound", err)
	}
}

func TestSubtreeIDs_Method(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	ids, err := svc.SubtreeIDs(context.Background(), 2)
	if err != nil {
		t.Fatalf("SubtreeIDs: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("ids = %v, want 3 ids (2,3,4)", ids)
	}
	set := map[int64]bool{}
	for _, id := range ids {
		set[id] = true
	}
	if !set[2] || !set[3] || !set[4] {
		t.Fatalf("ids = %v, want {2,3,4}", ids)
	}
}

func TestSubtreeIDs_MethodMissing(t *testing.T) {
	svc := newDeptSvc(newFakeDeptRepo(seedTree()...))
	if _, err := svc.SubtreeIDs(context.Background(), 99); !errors.Is(err, org.ErrDeptNotFound) {
		t.Fatalf("err = %v, want ErrDeptNotFound", err)
	}
}

func TestEnsureActive(t *testing.T) {
	rows := seedTree()
	rows[1].Status = 1 // a disabled
	svc := newDeptSvc(newFakeDeptRepo(rows...))
	ctx := context.Background()
	if err := svc.EnsureActive(ctx, 1); err != nil {
		t.Fatalf("EnsureActive(1): %v", err)
	}
	if err := svc.EnsureActive(ctx, 2); !errors.Is(err, org.ErrDeptDisabled) {
		t.Fatalf("EnsureActive(2) err = %v, want ErrDeptDisabled", err)
	}
	if err := svc.EnsureActive(ctx, 99); !errors.Is(err, org.ErrDeptNotFound) {
		t.Fatalf("EnsureActive(99) err = %v, want ErrDeptNotFound", err)
	}
}

func TestList_PropagatesRepoError(t *testing.T) {
	repo := newFakeDeptRepo()
	repo.listErr = errRepoFailed
	svc := newDeptSvc(repo)
	if _, err := svc.List(context.Background()); !errors.Is(err, errRepoFailed) {
		t.Fatalf("err = %v, want errRepoFailed", err)
	}
}

func TestBuildDeptTree(t *testing.T) {
	depts := []*entity.Dept{
		{ID: 2, ParentID: 1, DeptName: "a", OrderNum: 2},
		{ID: 3, ParentID: 1, DeptName: "b", OrderNum: 1},
		{ID: 4, ParentID: 3, DeptName: "b1", OrderNum: 1},
		{ID: 1, ParentID: 0, DeptName: "root", OrderNum: 1},
		{ID: 9, ParentID: 77, DeptName: "orphan", OrderNum: 1}, // parent missing
	}
	roots := BuildDeptTree(depts)
	if len(roots) != 1 || roots[0].ID != 1 {
		t.Fatalf("roots = %+v, want single root 1", roots)
	}
	root := roots[0]
	if len(root.Children) != 2 || root.Children[0].ID != 3 || root.Children[1].ID != 2 {
		t.Fatalf("children order = %+v, want [3 2] (order_num)", root.Children)
	}
	if len(root.Children[0].Children) != 1 || root.Children[0].Children[0].ID != 4 {
		t.Fatalf("grandchildren = %+v, want [4]", root.Children[0].Children)
	}
}

func TestAncestorIDs(t *testing.T) {
	depts := seedTree()
	ids, ok := AncestorIDs(depts, 3)
	if !ok || len(ids) != 2 || ids[0] != 2 || ids[1] != 1 {
		t.Fatalf("ids = %v ok = %v, want [2 1] true", ids, ok)
	}
	if _, ok := AncestorIDs(depts, 99); ok {
		t.Fatal("missing node reports ok")
	}
	// A broken chain (missing parent) reports not-ok so callers skip cascades.
	broken := []*entity.Dept{{ID: 3, ParentID: 2, DeptName: "a1"}}
	if _, ok := AncestorIDs(broken, 3); ok {
		t.Fatal("broken chain reports ok")
	}
}

func TestSubtreeIDs_Function(t *testing.T) {
	ids := SubtreeIDs(seedTree(), 1)
	if len(ids) != 5 {
		t.Fatalf("ids = %v, want 5 ids", ids)
	}
	if got := SubtreeIDs(seedTree(), 99); len(got) != 1 || got[0] != 99 {
		t.Fatalf("missing root ids = %v, want [99]", got)
	}
}

func TestHasEnabledDescendant(t *testing.T) {
	depts := seedTree()
	if !HasEnabledDescendant(depts, 1) {
		t.Fatal("root has enabled descendants")
	}
	if !HasEnabledDescendant(depts, 2) {
		t.Fatal("a has enabled descendants")
	}
	for _, d := range depts {
		if d.ID != 1 {
			d.Status = 1
		}
	}
	if HasEnabledDescendant(depts, 1) {
		t.Fatal("all descendants disabled")
	}
}
