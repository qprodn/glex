package service

import (
	"context"
	"errors"
	"testing"

	"glex/internal/domain/org"
	"glex/internal/domain/org/entity"
	"glex/internal/domain/org/repository"
)

// fakeUserPostRepo is an in-memory repository.UserPostRepo. users maps user
// id → department reference (existence = presence in the map); assigned maps
// user id → active post ids; postCounts backs the in-use guard.
type fakeUserPostRepo struct {
	users      map[int64]int64
	assigned   map[int64][]int64
	postCounts map[int64]int
	replaced   []replacedUserPosts
}

type replacedUserPosts struct {
	userID  int64
	postIDs []int64
	actorID int64
}

func newFakeUserPostRepo() *fakeUserPostRepo {
	return &fakeUserPostRepo{
		users:      make(map[int64]int64),
		assigned:   make(map[int64][]int64),
		postCounts: make(map[int64]int),
	}
}

func (r *fakeUserPostRepo) UserExists(_ context.Context, userID int64) (bool, error) {
	_, ok := r.users[userID]
	return ok, nil
}

func (r *fakeUserPostRepo) UserDeptID(_ context.Context, userID int64) (int64, error) {
	deptID, ok := r.users[userID]
	if !ok {
		return 0, org.ErrUserNotFound
	}
	return deptID, nil
}

func (r *fakeUserPostRepo) AssignedPostIDs(_ context.Context, userID int64) ([]int64, error) {
	return append([]int64(nil), r.assigned[userID]...), nil
}

func (r *fakeUserPostRepo) ReplaceUserPosts(_ context.Context, req repository.ReplaceUserPostsRequest) error {
	r.assigned[req.UserID] = append([]int64(nil), req.PostIDs...)
	r.replaced = append(r.replaced, replacedUserPosts{userID: req.UserID, postIDs: append([]int64(nil), req.PostIDs...), actorID: req.ActorID})
	return nil
}

func (r *fakeUserPostRepo) CountUserPosts(_ context.Context, postID int64) (int, error) {
	return r.postCounts[postID], nil
}

func seedPosts() []*entity.Post {
	return []*entity.Post{
		{ID: 21, DeptID: 2, PostCode: "fe", PostName: "前端开发", PostSort: 1, Status: 0},
		{ID: 22, DeptID: 2, PostCode: "be", PostName: "后端开发", PostSort: 2, Status: 0},
		{ID: 31, DeptID: 3, PostCode: "mkt", PostName: "市场专员", PostSort: 1, Status: 0},
	}
}

func newPostSvc(posts repository.PostRepo, depts repository.DeptRepo, userPosts repository.UserPostRepo) Post {
	return NewPostService(posts, depts, userPosts, testLogger())
}

func TestCreatePost(t *testing.T) {
	repo := newFakePostRepo(seedPosts()...)
	depts := newFakeDeptRepo(seedTree()...)
	svc := newPostSvc(repo, depts, newFakeUserPostRepo())

	created, err := svc.Create(context.Background(), &entity.Post{DeptID: 2, PostCode: "qa", PostName: "测试", PostSort: 3})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 || len(repo.created) != 1 {
		t.Fatalf("Create did not persist: %+v", created)
	}
}

func TestCreatePost_InvalidRejected(t *testing.T) {
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	for _, p := range []*entity.Post{
		{DeptID: 0, PostCode: "x", PostName: "x"},
		{DeptID: 2, PostCode: "", PostName: "x"},
		{DeptID: 2, PostCode: "x", PostName: ""},
	} {
		if _, err := svc.Create(context.Background(), p); !errors.Is(err, org.ErrPostInvalid) {
			t.Fatalf("err = %v, want ErrPostInvalid", err)
		}
	}
}

func TestCreatePost_MissingDeptRejected(t *testing.T) {
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	_, err := svc.Create(context.Background(), &entity.Post{DeptID: 99, PostCode: "x", PostName: "x"})
	if !errors.Is(err, org.ErrDeptNotFound) {
		t.Fatalf("err = %v, want ErrDeptNotFound", err)
	}
}

func TestCreatePost_NameTakenWithinDept(t *testing.T) {
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	_, err := svc.Create(context.Background(), &entity.Post{DeptID: 2, PostCode: "qa", PostName: "前端开发"})
	if !errors.Is(err, org.ErrPostNameTaken) {
		t.Fatalf("err = %v, want ErrPostNameTaken", err)
	}
}

func TestCreatePost_SameNameOtherDeptAllowed(t *testing.T) {
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	if _, err := svc.Create(context.Background(), &entity.Post{DeptID: 3, PostCode: "fe2", PostName: "前端开发"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreatePost_CodeTakenGlobally(t *testing.T) {
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	_, err := svc.Create(context.Background(), &entity.Post{DeptID: 3, PostCode: "fe", PostName: "别的名字"})
	if !errors.Is(err, org.ErrPostCodeTaken) {
		t.Fatalf("err = %v, want ErrPostCodeTaken", err)
	}
}

func TestUpdatePost_KeepsOwnNameAndCode(t *testing.T) {
	repo := newFakePostRepo(seedPosts()...)
	svc := newPostSvc(repo, newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	if _, err := svc.Update(context.Background(), &entity.Post{ID: 21, DeptID: 2, PostCode: "fe", PostName: "前端开发", PostSort: 9}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if repo.rows[21].PostSort != 9 {
		t.Fatalf("PostSort = %d, want 9", repo.rows[21].PostSort)
	}
}

func TestUpdatePost_NameTaken(t *testing.T) {
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	_, err := svc.Update(context.Background(), &entity.Post{ID: 21, DeptID: 2, PostCode: "fe", PostName: "后端开发"})
	if !errors.Is(err, org.ErrPostNameTaken) {
		t.Fatalf("err = %v, want ErrPostNameTaken", err)
	}
}

func TestUpdatePost_MoveToMissingDeptRejected(t *testing.T) {
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	_, err := svc.Update(context.Background(), &entity.Post{ID: 21, DeptID: 99, PostCode: "fe", PostName: "前端开发"})
	if !errors.Is(err, org.ErrDeptNotFound) {
		t.Fatalf("err = %v, want ErrDeptNotFound", err)
	}
}

func TestUpdatePost_DisableInUseRejected(t *testing.T) {
	userPosts := newFakeUserPostRepo()
	userPosts.postCounts[21] = 3
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), userPosts)
	_, err := svc.Update(context.Background(), &entity.Post{ID: 21, DeptID: 2, PostCode: "fe", PostName: "前端开发", Status: 1})
	if !errors.Is(err, org.ErrPostInUse) {
		t.Fatalf("err = %v, want ErrPostInUse", err)
	}
}

func TestUpdatePost_DisableAfterUnassigned(t *testing.T) {
	repo := newFakePostRepo(seedPosts()...)
	svc := newPostSvc(repo, newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	if _, err := svc.Update(context.Background(), &entity.Post{ID: 21, DeptID: 2, PostCode: "fe", PostName: "前端开发", Status: 1}); err != nil {
		t.Fatalf("Update disable: %v", err)
	}
	if repo.rows[21].Status != 1 {
		t.Fatalf("Status = %d, want 1", repo.rows[21].Status)
	}
}

func TestDeletePost_InUseRejected(t *testing.T) {
	userPosts := newFakeUserPostRepo()
	userPosts.postCounts[21] = 1
	repo := newFakePostRepo(seedPosts()...)
	svc := newPostSvc(repo, newFakeDeptRepo(seedTree()...), userPosts)
	if err := svc.Delete(context.Background(), DeletePostRequest{ID: 21, ActorID: 1}); !errors.Is(err, org.ErrPostInUse) {
		t.Fatalf("err = %v, want ErrPostInUse", err)
	}
	if len(repo.deleted) != 0 {
		t.Fatalf("deleted = %v, want none", repo.deleted)
	}
}

func TestDeletePost_Missing(t *testing.T) {
	svc := newPostSvc(newFakePostRepo(seedPosts()...), newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	if err := svc.Delete(context.Background(), DeletePostRequest{ID: 99, ActorID: 1}); !errors.Is(err, org.ErrPostNotFound) {
		t.Fatalf("err = %v, want ErrPostNotFound", err)
	}
}

func TestDeletePost(t *testing.T) {
	repo := newFakePostRepo(seedPosts()...)
	svc := newPostSvc(repo, newFakeDeptRepo(seedTree()...), newFakeUserPostRepo())
	if err := svc.Delete(context.Background(), DeletePostRequest{ID: 22, ActorID: 1}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 22 {
		t.Fatalf("deleted = %v, want [22]", repo.deleted)
	}
}
