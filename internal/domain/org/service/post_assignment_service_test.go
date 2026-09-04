package service

import (
	"context"
	"errors"
	"testing"

	"glex/internal/domain/org"
	"glex/internal/domain/org/repository"
)

func newAssignmentSvc(posts repository.PostRepo, userPosts repository.UserPostRepo) PostAssignment {
	return NewPostAssignmentService(posts, userPosts, testLogger())
}

func TestListForUser_MissingUserRejected(t *testing.T) {
	svc := newAssignmentSvc(newFakePostRepo(seedPosts()...), newFakeUserPostRepo())
	if _, err := svc.ListForUser(context.Background(), 99, 0); !errors.Is(err, org.ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestListForUser_FlagsAssignedCandidates(t *testing.T) {
	userPosts := newFakeUserPostRepo()
	userPosts.users[7] = 2 // user 7 sits in dept 2
	userPosts.assigned[7] = []int64{22}
	svc := newAssignmentSvc(newFakePostRepo(seedPosts()...), userPosts)

	items, err := svc.ListForUser(context.Background(), 7, 0)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	// Candidates are dept 2's active posts only (21, 22), ordered by sort.
	if len(items) != 2 || items[0].Post.ID != 21 || items[1].Post.ID != 22 {
		t.Fatalf("items = %+v, want posts 21,22", items)
	}
	if items[0].Assigned || !items[1].Assigned {
		t.Fatalf("assigned flags = %v/%v, want false/true", items[0].Assigned, items[1].Assigned)
	}
}

func TestListForUser_DeptOverrideChangesCandidates(t *testing.T) {
	userPosts := newFakeUserPostRepo()
	userPosts.users[7] = 2
	svc := newAssignmentSvc(newFakePostRepo(seedPosts()...), userPosts)

	items, err := svc.ListForUser(context.Background(), 7, 3)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(items) != 1 || items[0].Post.ID != 31 {
		t.Fatalf("items = %+v, want dept 3's post 31", items)
	}
}

func TestListForUser_ExcludesDisabledCandidates(t *testing.T) {
	posts := newFakePostRepo(seedPosts()...)
	posts.rows[22].Status = 1
	userPosts := newFakeUserPostRepo()
	userPosts.users[7] = 2
	userPosts.assigned[7] = []int64{22}
	svc := newAssignmentSvc(posts, userPosts)

	// Post 22 stays assigned (history) but is no longer a candidate.
	items, err := svc.ListForUser(context.Background(), 7, 0)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(items) != 1 || items[0].Post.ID != 21 {
		t.Fatalf("items = %+v, want only enabled post 21", items)
	}
}

func TestReplaceForUser_MissingUserRejected(t *testing.T) {
	svc := newAssignmentSvc(newFakePostRepo(seedPosts()...), newFakeUserPostRepo())
	if _, err := svc.ReplaceForUser(context.Background(), 99, []int64{21}, 1); !errors.Is(err, org.ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestReplaceForUser_UnknownPostRejected(t *testing.T) {
	userPosts := newFakeUserPostRepo()
	userPosts.users[7] = 2
	svc := newAssignmentSvc(newFakePostRepo(seedPosts()...), userPosts)
	if _, err := svc.ReplaceForUser(context.Background(), 7, []int64{21, 99}, 1); !errors.Is(err, org.ErrPostNotAssignable) {
		t.Fatalf("err = %v, want ErrPostNotAssignable", err)
	}
	if len(userPosts.replaced) != 0 {
		t.Fatalf("replaced = %+v, want none", userPosts.replaced)
	}
}

func TestReplaceForUser_DisabledPostRejected(t *testing.T) {
	posts := newFakePostRepo(seedPosts()...)
	posts.rows[21].Status = 1
	userPosts := newFakeUserPostRepo()
	userPosts.users[7] = 2
	svc := newAssignmentSvc(posts, userPosts)
	if _, err := svc.ReplaceForUser(context.Background(), 7, []int64{21}, 1); !errors.Is(err, org.ErrPostNotAssignable) {
		t.Fatalf("err = %v, want ErrPostNotAssignable", err)
	}
}

func TestReplaceForUser_DedupesAndAcceptsCrossDeptPost(t *testing.T) {
	// Posts from another department are accepted: which department a post
	// belongs to is not re-derived on save (RuoYi parity — the dialog's
	// options restrict the choices).
	userPosts := newFakeUserPostRepo()
	userPosts.users[7] = 2
	svc := newAssignmentSvc(newFakePostRepo(seedPosts()...), userPosts)

	if _, err := svc.ReplaceForUser(context.Background(), 7, []int64{21, 31, 21}, 9); err != nil {
		t.Fatalf("ReplaceForUser: %v", err)
	}
	if len(userPosts.replaced) != 1 {
		t.Fatalf("replaced %d times, want 1", len(userPosts.replaced))
	}
	got := userPosts.replaced[0]
	if len(got.postIDs) != 2 || got.postIDs[0] != 21 || got.postIDs[1] != 31 {
		t.Fatalf("postIDs = %v, want deduped [21 31]", got.postIDs)
	}
	if got.actorID != 9 {
		t.Fatalf("actorID = %d, want 9", got.actorID)
	}
}

func TestReplaceForUser_EmptyClears(t *testing.T) {
	userPosts := newFakeUserPostRepo()
	userPosts.users[7] = 2
	userPosts.assigned[7] = []int64{21}
	svc := newAssignmentSvc(newFakePostRepo(seedPosts()...), userPosts)

	if _, err := svc.ReplaceForUser(context.Background(), 7, nil, 1); err != nil {
		t.Fatalf("ReplaceForUser: %v", err)
	}
	if len(userPosts.assigned[7]) != 0 {
		t.Fatalf("assigned = %v, want cleared", userPosts.assigned[7])
	}
}
