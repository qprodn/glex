package org

import (
	"context"
	"log/slog"

	orgv1 "glex/api/org/v1"
	"glex/internal/bizpkg"
	orgentity "glex/internal/domain/org/entity"
	orgservice "glex/internal/domain/org/service"
	"glex/pkg/listq"
	"glex/pkg/logx"

	"go.einride.tech/aip/fieldmask"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// convertPost maps a orgentity.Post DO to the proto Post DTO.
func convertPost(p *orgentity.Post) *orgv1.Post {
	return &orgv1.Post{
		PostId:    p.ID,
		DeptId:    p.DeptID,
		PostCode:  p.PostCode,
		PostName:  p.PostName,
		PostSort:  int32(p.PostSort),
		Status:    p.Status,
		Remark:    p.Remark,
		CreateBy:  p.CreateBy,
		UpdateBy:  p.UpdateBy,
		CreatedAt: timestamppb.New(p.CreatedAt),
		UpdatedAt: timestamppb.New(p.UpdatedAt),
	}
}

// PostApplication implements the post CRUD service. All repository access
// goes through the post domain service; the department-subtree filter is
// resolved through the department domain service (same module — no contract)
// and handed to the listing as an id set. Authentication and the operation's
// Functional Permission policy are enforced by the registered HTTP
// middleware.
type PostApplication struct {
	orgv1.UnimplementedPostServiceServer

	svc    orgservice.Post
	depts  orgservice.Dept
	tx     Transaction
	logger *slog.Logger
}

// NewPostApplication creates a PostApplication. svc is the post domain
// service; depts resolves the optional department-subtree filter; tx wraps
// writes.
func NewPostApplication(svc orgservice.Post, depts orgservice.Dept, tx Transaction, logger *slog.Logger) *PostApplication {
	return &PostApplication{
		svc:    svc,
		depts:  depts,
		tx:     tx,
		logger: logger.With(slog.String("component", "application.org.post")),
	}
}

// ListPosts lists posts with AIP-160 filter, AIP-132 order, and page-token
// pagination. A non-zero dept_id restricts the listing to that department's
// subtree, resolved server-side by the department domain service.

// postListSpec is this module's list fact (ADR 0024): filterable
// idents plus the post listing order.
var postListSpec = listq.Spec{
	Idents: []filtering.DeclarationOption{
		filtering.DeclareIdent("post_code", filtering.TypeString),
		filtering.DeclareIdent("post_name", filtering.TypeString),
		filtering.DeclareIdent("status", filtering.TypeInt),
		filtering.DeclareIdent("dept_id", filtering.TypeInt),
	},
	DefaultOrder: []ordering.Field{{Path: "post_sort"}, {Path: "id"}},
}

func (s *PostApplication) ListPosts(ctx context.Context, req *orgv1.ListPostsRequest) (*orgv1.PostSet, error) {
	q, page, err := listq.Parse(req, postListSpec)
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.list_posts", err)
		return nil, err
	}
	var deptIDs []int64
	if req.GetDeptId() != 0 {
		deptIDs, err = s.depts.SubtreeIDs(ctx, req.GetDeptId())
		if err != nil {
			logx.Propagate(ctx, s.logger, "org.list_posts", err)
			return nil, err
		}
	}
	posts, err := s.svc.List(ctx, orgservice.ListPostsRequest{Query: q, DeptIDs: deptIDs})
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.list_posts", err)
		return nil, err
	}
	set := &orgv1.PostSet{Posts: make([]*orgv1.Post, 0, len(posts))}
	for _, p := range posts {
		set.Posts = append(set.Posts, convertPost(p))
	}
	set.NextPageToken = page.NextToken(len(posts))
	return set, nil
}

// GetPost retrieves a post by id.
func (s *PostApplication) GetPost(ctx context.Context, req *orgv1.GetPostRequest) (*orgv1.Post, error) {
	p, err := s.svc.Get(ctx, req.GetPostId())
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.get_post", err)
		return nil, err
	}
	return convertPost(p), nil
}

// CreatePost creates a post under an existing department; post_name is
// unique within the department, post_code globally.
func (s *PostApplication) CreatePost(ctx context.Context, req *orgv1.CreatePostRequest) (*orgv1.Post, error) {
	in := req.GetPost()
	actor := bizpkg.AuditActor(ctx)
	p := &orgentity.Post{
		DeptID:   in.GetDeptId(),
		PostCode: in.GetPostCode(),
		PostName: in.GetPostName(),
		PostSort: int(in.GetPostSort()),
		Status:   in.GetStatus(),
		Remark:   in.GetRemark(),
		CreateBy: actor,
		UpdateBy: actor,
	}
	var created *orgentity.Post
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.svc.Create(ctx, p)
		return err
	}); err != nil {
		logx.Propagate(ctx, s.logger, "org.create_post", err)
		return nil, err
	}
	return convertPost(created), nil
}

// UpdatePost partially updates a post via field-mask: it loads the current
// state as a DTO, applies the mask onto it, then persists. Disabling a post
// that still has users assigned is refused (RuoYi: 该岗位下存在已分配用户，
// 不能禁用).
func (s *PostApplication) UpdatePost(ctx context.Context, req *orgv1.UpdatePostRequest) (*orgv1.Post, error) {
	current, err := s.GetPost(ctx, &orgv1.GetPostRequest{PostId: req.GetPost().GetPostId()})
	if err != nil {
		logx.Propagate(ctx, s.logger, "org.update_post", err)
		return nil, err
	}
	fieldmask.Update(req.GetUpdateMask(), current, req.GetPost())
	p := &orgentity.Post{
		ID:       current.GetPostId(),
		DeptID:   current.GetDeptId(),
		PostCode: current.GetPostCode(),
		PostName: current.GetPostName(),
		PostSort: int(current.GetPostSort()),
		Status:   current.GetStatus(),
		Remark:   current.GetRemark(),
		UpdateBy: bizpkg.AuditActor(ctx),
	}
	var updated *orgentity.Post
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		updated, err = s.svc.Update(ctx, p)
		return err
	}); err != nil {
		logx.Propagate(ctx, s.logger, "org.update_post", err)
		return nil, err
	}
	return convertPost(updated), nil
}

// DeletePost soft-deletes a post by id; refused while users are still
// assigned (RuoYi: 已分配，不能删除).
func (s *PostApplication) DeletePost(ctx context.Context, req *orgv1.DeletePostRequest) (*emptypb.Empty, error) {
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		return s.svc.Delete(ctx, orgservice.DeletePostRequest{ID: req.GetPostId(), ActorID: bizpkg.AuditActor(ctx)})
	}); err != nil {
		logx.Propagate(ctx, s.logger, "org.delete_post", err)
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
