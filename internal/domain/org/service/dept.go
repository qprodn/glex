package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"glex/internal/domain/org"
	"glex/internal/domain/org/entity"
	"glex/internal/domain/org/repository"
	"glex/pkg/logx"
)

// Dept is the org domain service behind the department endpoints: the only
// path to the Department reads and writes (ADR 0005). Methods join the
// caller's ambient transaction; the Application service owns the transaction
// boundary. Every error a method returns to its caller first logs its Warn
// breadcrumb (ADR 0011).
type Dept interface {
	Get(ctx context.Context, id int64) (*entity.Dept, error)
	// List returns every non-withdrawn department as a flat list ordered by
	// (parent_id, order_num, id) — the input BuildDeptTree assembles.
	List(ctx context.Context) ([]*entity.Dept, error)
	// Create validates the department (name, parent exists and is enabled,
	// sibling name unique) and persists it.
	Create(ctx context.Context, d *entity.Dept) (*entity.Dept, error)
	// Update validates the change (shape, move cycle, sibling uniqueness,
	// disable guard) and persists it. Enabling a previously disabled
	// department enables its whole ancestor chain with it (RuoYi's
	// updateParentDeptStatusNormal).
	Update(ctx context.Context, d *entity.Dept) (*entity.Dept, error)
	// Delete soft-deletes the department identified by req, refusing the
	// seeded root and departments with children or posts.
	Delete(ctx context.Context, req repository.DeleteDeptRequest) error
	// SubtreeIDs resolves deptID plus every descendant id — the
	// department-subtree filter behind the org contract. Disabled
	// departments are included: disabling hides a department from selects,
	// not from its users.
	SubtreeIDs(ctx context.Context, deptID int64) ([]int64, error)
	// EnsureActive rejects departments that are missing or disabled — the
	// user-save validation behind the org contract.
	EnsureActive(ctx context.Context, deptID int64) error
	// DeptPaths maps the given department ids to their root-first display
	// paths ("总公司/研发部"), loading the tree once. Unknown or withdrawn
	// ids are absent from the map — callers degrade per id, they never fail
	// the page for one sour reference.
	DeptPaths(ctx context.Context, deptIDs []int64) (map[int64]string, error)
	// ResolveDeptPath maps a root-first display path back to the department
	// id — Data Import's department column (ADR 0017). Sibling names are
	// unique (assertSiblingNameUnique), so a full path names at most one
	// department. Unknown paths return DEPT_NOT_FOUND and disabled
	// departments DEPT_DISABLED, the org wire shape consumers surface.
	ResolveDeptPath(ctx context.Context, path string) (int64, error)
}

type deptImpl struct {
	depts  repository.DeptRepo
	posts  repository.PostRepo
	logger *slog.Logger
}

// NewDeptService creates the department domain service. posts backs the
// "a department with posts cannot be deleted" guard.
func NewDeptService(depts repository.DeptRepo, posts repository.PostRepo, logger *slog.Logger) Dept {
	return &deptImpl{
		depts:  depts,
		posts:  posts,
		logger: logger.With(slog.String("component", "domain.org.svc")),
	}
}

func (s *deptImpl) Get(ctx context.Context, id int64) (*entity.Dept, error) {
	d, err := s.depts.FindByID(ctx, id)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.get_dept", err)
		return nil, err
	}
	return d, nil
}

func (s *deptImpl) List(ctx context.Context) ([]*entity.Dept, error) {
	depts, err := s.depts.ListDepts(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.list_depts", err)
		return nil, err
	}
	return depts, nil
}

func (s *deptImpl) Create(ctx context.Context, d *entity.Dept) (*entity.Dept, error) {
	if err := d.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.create_dept", err)
		return nil, err
	}
	// A disabled department takes no children (RuoYi: 部门停用，不允许新增);
	// parent_id 0 (another root) is always accepted.
	if d.ParentID != 0 {
		parent, err := s.depts.FindByID(ctx, d.ParentID)
		if err != nil {
			logx.Propagate(ctx, s.logger, "org.svc.create_dept", err)
			return nil, err
		}
		if !parent.Enabled() {
			err := org.ErrDeptDisabled
			logx.Propagate(ctx, s.logger, "org.svc.create_dept", err)
			return nil, err
		}
	}
	if err := assertSiblingNameUnique(ctx, s.depts, d, false); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.create_dept", err)
		return nil, err
	}
	created, err := s.depts.CreateDept(ctx, d)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.create_dept", err)
		return nil, err
	}
	return created, nil
}

func (s *deptImpl) Update(ctx context.Context, d *entity.Dept) (*entity.Dept, error) {
	if err := d.Validate(); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_dept", err)
		return nil, err
	}
	current, err := s.depts.FindByID(ctx, d.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_dept", err)
		return nil, err
	}
	// One flat read feeds the move-cycle check, the disable guard, and the
	// enable cascade (dept tables are tens to hundreds of rows — ADR 0014).
	depts, err := s.depts.ListDepts(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_dept", err)
		return nil, err
	}
	if d.ParentID != current.ParentID {
		if err := assertMoveAllowed(depts, d); err != nil {
			logx.Propagate(ctx, s.logger, "org.svc.update_dept", err)
			return nil, err
		}
	}
	if err := assertSiblingNameUnique(ctx, s.depts, d, true); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_dept", err)
		return nil, err
	}
	// Disabling is refused while any descendant is still enabled (RuoYi:
	// 该部门包含未停用的子部门); the guard fires on the enabled→disabled
	// transition, so re-saving a disabled department never re-trips it.
	if current.Enabled() && !d.Enabled() && hasEnabledDescendant(depts, d.ID) {
		err := org.ErrDeptHasEnabledChildren
		logx.Propagate(ctx, s.logger, "org.svc.update_dept", err)
		return nil, err
	}
	result, err := s.depts.UpdateDept(ctx, d)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.update_dept", err)
		return nil, err
	}
	// Enabling a department brings its ancestor chain out of disabled with
	// it: a disabled ancestor would strand the department invisibly inside a
	// disabled subtree (RuoYi's updateParentDeptStatusNormal; the cascade
	// fires on the disabled→enabled transition only — the disable guards
	// keep enabled departments from ever having disabled ancestors).
	if !result.Previous.Enabled() && result.Dept.Enabled() {
		if ancestors := ancestorIDs(depts, result.Dept.ID); len(ancestors) > 0 {
			if err := s.depts.EnableDepts(ctx, repository.EnableDeptsRequest{IDs: ancestors, ActorID: d.UpdateBy}); err != nil {
				logx.Propagate(ctx, s.logger, "org.svc.update_dept", err)
				return nil, err
			}
		}
	}
	return result.Dept, nil
}

func (s *deptImpl) Delete(ctx context.Context, req repository.DeleteDeptRequest) error {
	if _, err := s.depts.FindByID(ctx, req.ID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.delete_dept", err)
		return err
	}
	if req.ID == org.RootDeptID {
		err := org.ErrDeptRootProtected
		logx.Propagate(ctx, s.logger, "org.svc.delete_dept", err)
		return err
	}
	n, err := s.depts.CountChildDepts(ctx, req.ID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.delete_dept", err)
		return err
	}
	if n > 0 {
		err := org.ErrDeptHasChildren
		logx.Propagate(ctx, s.logger, "org.svc.delete_dept", err)
		return err
	}
	// A department that still carries posts is not withdrawable (RuoYi 6.0:
	// 部门存在岗位) — withdrawing it would strand the posts.
	if n, err := s.posts.CountByDept(ctx, req.ID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.delete_dept", err)
		return err
	} else if n > 0 {
		err := org.ErrDeptHasPosts
		logx.Propagate(ctx, s.logger, "org.svc.delete_dept", err)
		return err
	}
	if err := s.depts.DeleteDept(ctx, req); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.delete_dept", err)
		return err
	}
	return nil
}

func (s *deptImpl) SubtreeIDs(ctx context.Context, deptID int64) ([]int64, error) {
	if _, err := s.depts.FindByID(ctx, deptID); err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.subtree_ids", err)
		return nil, err
	}
	depts, err := s.depts.ListDepts(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.subtree_ids", err)
		return nil, err
	}
	return subtreeIDs(depts, deptID), nil
}

func (s *deptImpl) EnsureActive(ctx context.Context, deptID int64) error {
	d, err := s.depts.FindByID(ctx, deptID)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.ensure_active", err)
		return err
	}
	if !d.Enabled() {
		err := org.ErrDeptDisabled
		logx.Propagate(ctx, s.logger, "org.svc.ensure_active", err)
		return err
	}
	return nil
}

func (s *deptImpl) DeptPaths(ctx context.Context, deptIDs []int64) (map[int64]string, error) {
	depts, err := s.depts.ListDepts(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.dept_paths", err)
		return nil, err
	}
	wanted := make(map[int64]struct{}, len(deptIDs))
	for _, id := range deptIDs {
		wanted[id] = struct{}{}
	}
	byID := make(map[int64]*entity.Dept, len(depts))
	for _, d := range depts {
		byID[d.ID] = d
	}
	paths := make(map[int64]string, len(deptIDs))
	for id := range wanted {
		if path, ok := deptPath(byID, id); ok {
			paths[id] = path
		}
	}
	return paths, nil
}

func (s *deptImpl) ResolveDeptPath(ctx context.Context, path string) (int64, error) {
	depts, err := s.depts.ListDepts(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.svc.resolve_dept_path", err)
		return 0, err
	}
	byID := make(map[int64]*entity.Dept, len(depts))
	for _, d := range depts {
		byID[d.ID] = d
	}
	for _, d := range depts {
		if p, ok := deptPath(byID, d.ID); ok && p == path {
			if !d.Enabled() {
				err := org.ErrDeptDisabled
				logx.Propagate(ctx, s.logger, "org.svc.resolve_dept_path", err)
				return 0, err
			}
			return d.ID, nil
		}
	}
	err = org.ErrDeptNotFound
	logx.Propagate(ctx, s.logger, "org.svc.resolve_dept_path", err)
	return 0, err
}

// deptPath walks a department's parent chain to the root and joins the
// names root-first with "/". A broken chain (unknown parent, or a cycle the
// move guard should have prevented) yields ok=false — the reference degrades,
// the read never fails on it.
func deptPath(byID map[int64]*entity.Dept, deptID int64) (string, bool) {
	var names []string
	visited := map[int64]struct{}{}
	for id := deptID; ; {
		d, ok := byID[id]
		if !ok {
			return "", false
		}
		if _, seen := visited[id]; seen {
			return "", false
		}
		visited[id] = struct{}{}
		names = append(names, d.DeptName)
		if d.ParentID == 0 {
			break
		}
		id = d.ParentID
	}
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	// The ASCII separator has one spelling across the exported cell and the
	// import that reads it back.
	return strings.Join(names, "/"), true
}

// assertMoveAllowed rejects moving a department under itself or one of its
// descendants — the move would fold its own subtree into a cycle (RuoYi:
// 上级部门不能是自己). Any other existing parent accepts the move; whether it
// is enabled is the mover's choice, as in RuoYi.
func assertMoveAllowed(depts []*entity.Dept, d *entity.Dept) error {
	byID := make(map[int64]*entity.Dept, len(depts))
	for _, node := range depts {
		byID[node.ID] = node
	}
	if d.ParentID == d.ID {
		return org.ErrDeptParentInvalid
	}
	parent, ok := byID[d.ParentID]
	if !ok {
		return org.ErrDeptNotFound
	}
	for _, id := range ancestorIDs(depts, parent.ID) {
		if id == d.ID {
			return org.ErrDeptParentInvalid
		}
	}
	return nil
}

// assertSiblingNameUnique checks no other department shares
// (parent_id, dept_name). When excludeSelf is set (update), a match bearing
// the same id is allowed. A not-found from the repo is the expected
// "available" case. Its caller's method boundary owns the breadcrumb
// (ADR 0011). A package-level invariant helper, unexported rather than
// conformed (ADR 0025).
func assertSiblingNameUnique(ctx context.Context, repo repository.DeptRepo, d *entity.Dept, excludeSelf bool) error {
	existing, err := repo.FindByParentAndName(ctx, repository.FindByParentAndNameRequest{ParentID: d.ParentID, Name: d.DeptName})
	if err != nil {
		if errors.Is(err, org.ErrDeptNotFound) {
			return nil
		}
		return err
	}
	if excludeSelf && existing.ID == d.ID {
		return nil
	}
	return org.ErrDeptNameTaken
}

// BuildDeptTree assembles a flat list into a forest in a single pass: roots
// are nodes whose parent_id == 0; each node pulls its children from a map
// keyed by parent id; sibling groups are sorted by order_num (BuildMenuTree's
// shape — ADR 0014). Orphans — whose parent is missing (e.g. soft-deleted) —
// are dropped from the tree (they remain in the store and are still reachable
// by id).
func BuildDeptTree(depts []*entity.Dept) []*entity.Dept {
	byParent := make(map[int64][]*entity.Dept)
	for _, d := range depts {
		byParent[d.ParentID] = append(byParent[d.ParentID], d)
	}
	for _, group := range byParent {
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].OrderNum < group[j].OrderNum
		})
	}
	roots := byParent[0]
	var attach func(nodes []*entity.Dept)
	attach = func(nodes []*entity.Dept) {
		for _, n := range nodes {
			n.Children = byParent[n.ID]
			attach(n.Children)
		}
	}
	attach(roots)
	return roots
}

// ancestorIDs walks the parent chain from deptID upward, returning the ids
// strictly above it. A nil result means deptID is missing from the list or
// its chain is broken (a soft-deleted ancestor): the caller treats the node
// as an orphan and skips whatever cascade it was about to run. A cyclic
// chain also yields nil — it cannot be trusted.
func ancestorIDs(depts []*entity.Dept, deptID int64) []int64 {
	byID := make(map[int64]*entity.Dept, len(depts))
	for _, d := range depts {
		byID[d.ID] = d
	}
	node, ok := byID[deptID]
	if !ok {
		return nil
	}
	seen := map[int64]struct{}{deptID: {}}
	var ids []int64
	for parentID := node.ParentID; parentID != 0; {
		if _, cycle := seen[parentID]; cycle {
			return nil
		}
		seen[parentID] = struct{}{}
		parent, exists := byID[parentID]
		if !exists {
			return nil
		}
		ids = append(ids, parentID)
		parentID = parent.ParentID
	}
	return ids
}

// subtreeIDs resolves deptID plus every descendant id (any depth, any
// status) from the flat list. A missing root yields just deptID itself —
// callers gate existence separately. Each node is visited at most once, so a
// cycle cannot duplicate ids.
func subtreeIDs(depts []*entity.Dept, deptID int64) []int64 {
	byParent := make(map[int64][]*entity.Dept)
	for _, d := range depts {
		byParent[d.ParentID] = append(byParent[d.ParentID], d)
	}
	ids := make([]int64, 0, 1)
	var walk func(id int64)
	walk = func(id int64) {
		ids = append(ids, id)
		for _, child := range byParent[id] {
			walk(child.ID)
		}
	}
	walk(deptID)
	return ids
}

// hasEnabledDescendant reports whether any department below deptID is still
// enabled — the disable guard (RuoYi: 该部门包含未停用的子部门). Direct and
// deeper descendants both count, matching RuoYi's find_in_set depth.
func hasEnabledDescendant(depts []*entity.Dept, deptID int64) bool {
	byParent := make(map[int64][]*entity.Dept)
	for _, d := range depts {
		byParent[d.ParentID] = append(byParent[d.ParentID], d)
	}
	queue := append([]*entity.Dept(nil), byParent[deptID]...)
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if node.Enabled() {
			return true
		}
		queue = append(queue, byParent[node.ID]...)
	}
	return false
}
