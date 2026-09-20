package org

import (
	"context"
	"log/slog"

	orgv1 "glex/api/org/v1"
	"glex/internal/bizpkg"
	"glex/internal/crossdomain/user"
	orgdomain "glex/internal/domain/org"
	orgentity "glex/internal/domain/org/entity"
	orgrepository "glex/internal/domain/org/repository"
	orgservice "glex/internal/domain/org/service"
	"glex/pkg/logx"

	"go.einride.tech/aip/fieldmask"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertDept maps a orgentity.Dept DO to the proto Dept DTO, recursing into
// the entity's Children (populated by the domain's BuildDeptTree).
func convertDept(d *orgentity.Dept) *orgv1.Dept {
	out := &orgv1.Dept{
		DeptId:    d.ID,
		ParentId:  d.ParentID,
		DeptName:  d.DeptName,
		OrderNum:  int32(d.OrderNum),
		Leader:    d.Leader,
		Phone:     d.Phone,
		Email:     d.Email,
		Status:    d.Status,
		CreateBy:  d.CreateBy,
		UpdateBy:  d.UpdateBy,
		CreatedAt: timestamppb.New(d.CreatedAt),
		UpdatedAt: timestamppb.New(d.UpdatedAt),
	}
	if len(d.Children) > 0 {
		out.Children = make([]*orgv1.Dept, 0, len(d.Children))
		for _, child := range d.Children {
			out.Children = append(out.Children, convertDept(child))
		}
	}
	return out
}

// convertDeptTreeOption maps one node of the entity forest to the
// lightweight select shape (label + disabled only).
func convertDeptTreeOption(d *orgentity.Dept) *orgv1.DeptTreeOption {
	out := &orgv1.DeptTreeOption{
		DeptId:   d.ID,
		ParentId: d.ParentID,
		Label:    d.DeptName,
		Disabled: !d.Enabled(),
	}
	if len(d.Children) > 0 {
		out.Children = make([]*orgv1.DeptTreeOption, 0, len(d.Children))
		for _, child := range d.Children {
			out.Children = append(out.Children, convertDeptTreeOption(child))
		}
	}
	return out
}

// DeptApplication implements the department CRUD + tree service. All
// repository access goes through the department domain service; the tree and
// guard rules live there too (ADR 0005). Authentication and the operation's
// Functional Permission policy are enforced by the registered HTTP
// middleware.
type DeptApplication struct {
	orgv1.UnimplementedDeptServiceServer

	svc    orgservice.Dept
	tx     Transaction
	usage  user.DeptUsageReader
	logger *slog.Logger
}

// NewDeptApplication creates a DeptApplication. svc is the department domain
// service — the module's only repository holder; tx wraps writes that span
// more than one statement (the enable cascade, the guards' reads); usage
// asks the user module, through the crossdomain contract, whether a
// department is still referenced by users — the guard the org module cannot
// check on its own tables (ADR 0013).
func NewDeptApplication(svc orgservice.Dept, tx Transaction, usage user.DeptUsageReader, logger *slog.Logger) *DeptApplication {
	return &DeptApplication{
		svc:    svc,
		tx:     tx,
		usage:  usage,
		logger: logger.With(slog.String("component", "application.org.dept")),
	}
}

// ListDepts returns the full department tree — every non-withdrawn
// department, server-built by the domain's BuildDeptTree and ordered by
// order_num (ADR 0014).
func (s *DeptApplication) ListDepts(ctx context.Context, _ *emptypb.Empty) (*orgv1.DeptTree, error) {
	depts, err := s.svc.List(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.list_depts", err)
		return nil, err
	}
	roots := orgservice.BuildDeptTree(depts)
	tree := &orgv1.DeptTree{Depts: make([]*orgv1.Dept, 0, len(roots))}
	for _, root := range roots {
		tree.Depts = append(tree.Depts, convertDept(root))
	}
	return tree, nil
}

// GetDept retrieves a department by id.
func (s *DeptApplication) GetDept(ctx context.Context, req *orgv1.GetDeptRequest) (*orgv1.Dept, error) {
	d, err := s.svc.Get(ctx, req.GetDeptId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.get_dept", err)
		return nil, err
	}
	return convertDept(d), nil
}

// CreateDept creates a department under an existing, enabled parent
// (parent_id 0 creates another root). dept_name must be unique among
// siblings.
func (s *DeptApplication) CreateDept(ctx context.Context, req *orgv1.CreateDeptRequest) (*orgv1.Dept, error) {
	in := req.GetDept()
	actor := bizpkg.AuditActor(ctx)
	d := &orgentity.Dept{
		ParentID: in.GetParentId(),
		DeptName: in.GetDeptName(),
		OrderNum: int(in.GetOrderNum()),
		Leader:   in.GetLeader(),
		Phone:    in.GetPhone(),
		Email:    in.GetEmail(),
		Status:   in.GetStatus(),
		CreateBy: actor,
		UpdateBy: actor,
	}
	var created *orgentity.Dept
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.svc.Create(ctx, d)
		return err
	}); err != nil {
		logx.Propagate(ctx, s.logger, "org.create_dept", err)
		return nil, err
	}
	return convertDept(created), nil
}

// UpdateDept partially updates a department via field-mask: it loads the
// current state as a DTO, applies the mask onto it, then persists. Moving a
// department is an ordinary parent_id update; enabling one enables its
// ancestors; disabling one with enabled descendants or users is refused.
func (s *DeptApplication) UpdateDept(ctx context.Context, req *orgv1.UpdateDeptRequest) (*orgv1.Dept, error) {
	current, err := s.GetDept(ctx, &orgv1.GetDeptRequest{DeptId: req.GetDept().GetDeptId()})
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.update_dept", err)
		return nil, err
	}
	// Capture the pre-mask state first: fieldmask.Update mutates `current`
	// in place, so afterwards it no longer testifies what the record was.
	wasEnabled := current.GetStatus() == 0
	fieldmask.Update(req.GetUpdateMask(), current, req.GetDept())
	disabling := wasEnabled && current.GetStatus() == 1
	d := &orgentity.Dept{
		ID:       current.GetDeptId(),
		ParentID: current.GetParentId(),
		DeptName: current.GetDeptName(),
		OrderNum: int(current.GetOrderNum()),
		Leader:   current.GetLeader(),
		Phone:    current.GetPhone(),
		Email:    current.GetEmail(),
		Status:   current.GetStatus(),
		UpdateBy: bizpkg.AuditActor(ctx),
	}
	var updated *orgentity.Dept
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		// Disabling is refused while users still sit in the department
		// (RuoYi: 该部门下存在已分配用户，不能禁用) — asked through the
		// crossdomain user contract inside the transaction.
		if disabling {
			if has, err := s.usage.HasUsersInDept(ctx, d.ID); err != nil {
				return err
			} else if has {
				return orgdomain.ErrDeptHasUsers
			}
		}
		var err error
		updated, err = s.svc.Update(ctx, d)
		return err
	}); err != nil {
		logx.Propagate(ctx, s.logger, "org.update_dept", err)
		return nil, err
	}
	return convertDept(updated), nil
}

// DeleteDept soft-deletes a department by id; refused for the seeded root,
// while children, users, or posts still exist.
func (s *DeptApplication) DeleteDept(ctx context.Context, req *orgv1.DeleteDeptRequest) (*emptypb.Empty, error) {
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		// The user guard runs first, through the crossdomain user contract
		// inside the transaction (RuoYi: 部门存在用户,不允许删除); the
		// children/posts guards live in the domain service.
		if has, err := s.usage.HasUsersInDept(ctx, req.GetDeptId()); err != nil {
			return err
		} else if has {
			return orgdomain.ErrDeptHasUsers
		}
		return s.svc.Delete(ctx, orgrepository.DeleteDeptRequest{ID: req.GetDeptId(), ActorID: bizpkg.AuditActor(ctx)})
	}); err != nil {
		logx.Propagate(ctx, s.logger, "org.delete_dept", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// ListDeptTree returns the lightweight department tree the selects render
// from — every non-withdrawn department as {id, label, disabled}, disabled
// nodes carried (not dropped) so a select can grey them out (RuoYi's
// buildDeptTreeSelect).
func (s *DeptApplication) ListDeptTree(ctx context.Context, _ *emptypb.Empty) (*orgv1.DeptTreeSelect, error) {
	depts, err := s.svc.List(ctx)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.list_dept_tree", err)
		return nil, err
	}
	roots := orgservice.BuildDeptTree(depts)
	selectTree := &orgv1.DeptTreeSelect{Options: make([]*orgv1.DeptTreeOption, 0, len(roots))}
	for _, root := range roots {
		selectTree.Options = append(selectTree.Options, convertDeptTreeOption(root))
	}
	return selectTree, nil
}
