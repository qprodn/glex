package org

import (
	"context"
	"errors"
	"testing"
	"time"

	orgv1 "glex/api/org/v1"
	orgentity "glex/internal/domain/org/entity"
	orgservice "glex/internal/domain/org/service"

	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fakePostService is a hand-rolled orgservice.Post fake: it records calls
// and returns configured results, so the tests exercise orchestration only.
type fakePostService struct {
	get       *orgentity.Post
	create    *orgentity.Post
	update    *orgentity.Post
	getErr    error
	createErr error
	updateErr error
	deleteErr error

	createCalled int
	createCtx    context.Context
	createInput  *orgentity.Post
	updateCalled int
	updateInput  *orgentity.Post
	deleteCalled int
	deleteID     int64
	deleteActor  int64
	deleteCtx    context.Context
}

func (f *fakePostService) Get(_ context.Context, _ int64) (*orgentity.Post, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.get, nil
}

func (f *fakePostService) List(context.Context, orgservice.ListPostsRequest) ([]*orgentity.Post, error) {
	return nil, nil
}

func (f *fakePostService) Create(ctx context.Context, p *orgentity.Post) (*orgentity.Post, error) {
	f.createCalled++
	f.createCtx, f.createInput = ctx, p
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.create, nil
}

func (f *fakePostService) Update(_ context.Context, p *orgentity.Post) (*orgentity.Post, error) {
	f.updateCalled++
	f.updateInput = p
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.update, nil
}

func (f *fakePostService) Delete(ctx context.Context, req orgservice.DeletePostRequest) error {
	f.deleteCalled++
	f.deleteCtx, f.deleteID, f.deleteActor = ctx, req.ID, req.ActorID
	return f.deleteErr
}

// fakeDeptServiceForPost is the orgservice.Dept shape PostApplication
// consumes: the subtree resolution behind the dept_id list parameter.
type fakeDeptServiceForPost struct {
	subtreeIDs []int64
	subtreeErr error
	askedRoot  int64
}

func (f *fakeDeptServiceForPost) Get(context.Context, int64) (*orgentity.Dept, error) {
	return nil, nil
}

func (f *fakeDeptServiceForPost) List(context.Context) ([]*orgentity.Dept, error) {
	return nil, nil
}

func (f *fakeDeptServiceForPost) Create(context.Context, *orgentity.Dept) (*orgentity.Dept, error) {
	return nil, nil
}

func (f *fakeDeptServiceForPost) Update(context.Context, *orgentity.Dept) (*orgentity.Dept, error) {
	return nil, nil
}

func (f *fakeDeptServiceForPost) Delete(context.Context, orgservice.DeleteDeptRequest) error {
	return nil
}

func (f *fakeDeptServiceForPost) SubtreeIDs(_ context.Context, root int64) ([]int64, error) {
	f.askedRoot = root
	return f.subtreeIDs, f.subtreeErr
}

func (f *fakeDeptServiceForPost) EnsureActive(context.Context, int64) error {
	return nil
}

func (f *fakeDeptServiceForPost) DeptPaths(context.Context, []int64) (map[int64]string, error) {
	return nil, nil
}

func (f *fakeDeptServiceForPost) ResolveDeptPath(context.Context, string) (int64, error) {
	return 0, nil
}

func newPostApp(svc *fakePostService, depts *fakeDeptServiceForPost) (*PostApplication, *fakeTransaction) {
	tx := newFakeTransaction()
	return NewPostApplication(svc, depts, tx, testLogger()), tx
}

func TestCreatePost_StampsActorAndRunsInTx(t *testing.T) {
	svc := &fakePostService{create: &orgentity.Post{ID: 21, PostCode: "fe", PostName: "前端开发"}}
	app, tx := newPostApp(svc, new(fakeDeptServiceForPost))

	got, err := app.CreatePost(actorContext(42), &orgv1.CreatePostRequest{
		Post: &orgv1.Post{DeptId: 2, PostCode: "fe", PostName: "前端开发", PostSort: 1},
	})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if got.PostId != 21 {
		t.Fatalf("PostId = %d, want 21", got.PostId)
	}
	if tx.began != 1 || !inTx(svc.createCtx) {
		t.Fatal("Create did not run inside the transaction")
	}
	if svc.createInput.CreateBy != 42 || svc.createInput.PostSort != 1 {
		t.Fatalf("input = %+v", svc.createInput)
	}
}

func TestGetPost(t *testing.T) {
	svc := &fakePostService{get: &orgentity.Post{ID: 21, DeptID: 2, PostCode: "fe", PostName: "前端开发", CreatedAt: time.Unix(0, 0)}}
	app, _ := newPostApp(svc, new(fakeDeptServiceForPost))
	got, err := app.GetPost(context.Background(), &orgv1.GetPostRequest{PostId: 21})
	if err != nil {
		t.Fatalf("GetPost: %v", err)
	}
	if got.PostId != 21 || got.DeptId != 2 || got.CreatedAt.AsTime().Unix() != 0 {
		t.Fatalf("got = %+v", got)
	}
}

func TestUpdatePost_AppliesMaskOntoCurrent(t *testing.T) {
	svc := &fakePostService{
		get:    &orgentity.Post{ID: 21, DeptID: 2, PostCode: "fe", PostName: "前端开发", PostSort: 1},
		update: &orgentity.Post{ID: 21, PostName: "前端开发组"},
	}
	app, _ := newPostApp(svc, new(fakeDeptServiceForPost))

	if _, err := app.UpdatePost(actorContext(9), &orgv1.UpdatePostRequest{
		Post:       &orgv1.Post{PostId: 21, PostName: "前端开发组"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"post_name"}},
	}); err != nil {
		t.Fatalf("UpdatePost: %v", err)
	}
	in := svc.updateInput
	if in.PostName != "前端开发组" {
		t.Fatalf("PostName = %q, want masked value", in.PostName)
	}
	if in.PostCode != "fe" || in.DeptID != 2 || in.PostSort != 1 {
		t.Fatalf("unmasked fields drifted: %+v", in)
	}
	if in.UpdateBy != 9 {
		t.Fatalf("UpdateBy = %d, want 9", in.UpdateBy)
	}
}

func TestDeletePost_PassesIDActorAndTx(t *testing.T) {
	svc := &fakePostService{}
	app, tx := newPostApp(svc, new(fakeDeptServiceForPost))
	if _, err := app.DeletePost(actorContext(7), &orgv1.DeletePostRequest{PostId: 21}); err != nil {
		t.Fatalf("DeletePost: %v", err)
	}
	if tx.began != 1 || svc.deleteID != 21 || svc.deleteActor != 7 || !inTx(svc.deleteCtx) {
		t.Fatalf("tx=%d id=%d actor=%d", tx.began, svc.deleteID, svc.deleteActor)
	}
}

func TestListPosts_ResolvesDeptSubtree(t *testing.T) {
	// The subtree resolution is exercised through the dept service fake; the
	// listing itself is a passthrough, covered by the pattern shared with
	// role/dict.
	depts := &fakeDeptServiceForPost{subtreeIDs: []int64{2, 3, 4}}
	app, _ := newPostApp(new(fakePostService), depts)
	if _, err := app.ListPosts(context.Background(), &orgv1.ListPostsRequest{DeptId: 2}); err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if depts.askedRoot != 2 {
		t.Fatalf("askedRoot = %d, want 2", depts.askedRoot)
	}
}

func TestListPosts_SubtreeErrorPropagates(t *testing.T) {
	depts := &fakeDeptServiceForPost{subtreeErr: errServiceFailed}
	app, _ := newPostApp(new(fakePostService), depts)
	if _, err := app.ListPosts(context.Background(), &orgv1.ListPostsRequest{DeptId: 2}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("err = %v, want errServiceFailed", err)
	}
}

func TestListPosts_ZeroDeptSkipsSubtree(t *testing.T) {
	depts := &fakeDeptServiceForPost{}
	app, _ := newPostApp(new(fakePostService), depts)
	if _, err := app.ListPosts(context.Background(), &orgv1.ListPostsRequest{}); err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if depts.askedRoot != 0 {
		t.Fatalf("askedRoot = %d, want 0 (no resolution)", depts.askedRoot)
	}
}

// Compile-time check that the fakes satisfy the service interfaces.
var (
	_ orgservice.Post = (*fakePostService)(nil)
	_ orgservice.Dept = (*fakeDeptServiceForPost)(nil)
)
