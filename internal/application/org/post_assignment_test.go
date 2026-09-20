package org

import (
	"context"
	"errors"
	"testing"

	orgv1 "glex/api/org/v1"
	orgentity "glex/internal/domain/org/entity"
	orgrepository "glex/internal/domain/org/repository"
	orgservice "glex/internal/domain/org/service"
)

// fakePostAssignmentService is a hand-rolled orgservice.PostAssignment fake:
// it records calls and returns configured results.
type fakePostAssignmentService struct {
	list       []*orgentity.PostAssignment
	replaced   []*orgentity.PostAssignment
	listErr    error
	replaceErr error

	listedUser    int64
	listedDept    int64
	replaceCalled int
	replaceCtx    context.Context
	replacedUser  int64
	replacedIDs   []int64
	replacedActor int64
}

func (f *fakePostAssignmentService) ListForUser(_ context.Context, req orgservice.ListForUserRequest) ([]*orgentity.PostAssignment, error) {
	f.listedUser, f.listedDept = req.UserID, req.DeptOverride
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

func (f *fakePostAssignmentService) ReplaceForUser(ctx context.Context, req orgrepository.ReplaceUserPostsRequest) ([]*orgentity.PostAssignment, error) {
	f.replaceCalled++
	f.replaceCtx, f.replacedUser, f.replacedIDs, f.replacedActor = ctx, req.UserID, append([]int64(nil), req.PostIDs...), req.ActorID
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	return f.replaced, nil
}

func newPostAssignmentApp(svc *fakePostAssignmentService) (*PostAssignmentApplication, *fakeTransaction) {
	tx := newFakeTransaction()
	return NewPostAssignmentApplication(svc, tx, testLogger()), tx
}

func TestListUserPosts_PassesOverrideThrough(t *testing.T) {
	svc := &fakePostAssignmentService{list: []*orgentity.PostAssignment{
		{Post: &orgentity.Post{ID: 21, PostName: "前端开发"}, Assigned: true},
	}}
	app, _ := newPostAssignmentApp(svc)

	got, err := app.ListUserPosts(context.Background(), &orgv1.ListUserPostsRequest{UserId: 7, DeptId: 3})
	if err != nil {
		t.Fatalf("ListUserPosts: %v", err)
	}
	if svc.listedUser != 7 || svc.listedDept != 3 {
		t.Fatalf("asked user/dept = %d/%d, want 7/3", svc.listedUser, svc.listedDept)
	}
	if len(got.Items) != 1 || !got.Items[0].Assigned || got.Items[0].Post.PostId != 21 {
		t.Fatalf("items = %+v", got.Items)
	}
}

func TestListUserPosts_PropagatesServiceError(t *testing.T) {
	app, _ := newPostAssignmentApp(&fakePostAssignmentService{listErr: errServiceFailed})
	if _, err := app.ListUserPosts(context.Background(), &orgv1.ListUserPostsRequest{UserId: 7}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("err = %v, want errServiceFailed", err)
	}
}

func TestUpdateUserPosts_ReplacesInTx(t *testing.T) {
	svc := &fakePostAssignmentService{replaced: []*orgentity.PostAssignment{}}
	app, tx := newPostAssignmentApp(svc)

	if _, err := app.UpdateUserPosts(actorContext(5), &orgv1.UpdateUserPostsRequest{
		UserId:  7,
		PostIds: []int64{21, 22},
	}); err != nil {
		t.Fatalf("UpdateUserPosts: %v", err)
	}
	if tx.began != 1 || svc.replaceCalled != 1 || !inTx(svc.replaceCtx) {
		t.Fatal("replace did not run inside the transaction")
	}
	if svc.replacedUser != 7 || svc.replacedActor != 5 {
		t.Fatalf("user/actor = %d/%d, want 7/5", svc.replacedUser, svc.replacedActor)
	}
	if len(svc.replacedIDs) != 2 || svc.replacedIDs[0] != 21 {
		t.Fatalf("postIDs = %v", svc.replacedIDs)
	}
}

func TestUpdateUserPosts_PropagatesServiceError(t *testing.T) {
	app, _ := newPostAssignmentApp(&fakePostAssignmentService{replaceErr: errServiceFailed})
	if _, err := app.UpdateUserPosts(actorContext(1), &orgv1.UpdateUserPostsRequest{UserId: 7}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("err = %v, want errServiceFailed", err)
	}
}
