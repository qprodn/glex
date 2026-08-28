package service

import (
	"context"
	"testing"

	"glex/internal/domain/user"
	"glex/internal/domain/user/entity"
	"glex/internal/domain/user/repository"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeUserRepo is an in-memory repository.UserRepo for testing the domain
// service in isolation from the infrastructure layer. It mirrors the active-row
// lookup semantics the uniqueness checks rely on: lookups miss soft-deleted
// values, and a miss returns ErrUserNotFound (the "available" case).
type fakeUserRepo struct {
	byID    map[int64]*entity.User
	byName  map[string]*entity.User
	byEmail map[string]*entity.User
	byPhone map[string]*entity.User
	nextID  int64
	created []*entity.User
	deleted []int64
	actors  []int64
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byID:    make(map[int64]*entity.User),
		byName:  make(map[string]*entity.User),
		byEmail: make(map[string]*entity.User),
		byPhone: make(map[string]*entity.User),
		nextID:  1,
	}
}

var _ repository.UserRepo = (*fakeUserRepo)(nil)

func (r *fakeUserRepo) FindByID(_ context.Context, id int64) (*entity.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, user.ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByUserName(_ context.Context, name string) (*entity.User, error) {
	u, ok := r.byName[name]
	if !ok {
		return nil, user.ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (*entity.User, error) {
	u, ok := r.byEmail[email]
	if !ok {
		return nil, user.ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByPhoneNumber(_ context.Context, phone string) (*entity.User, error) {
	u, ok := r.byPhone[phone]
	if !ok {
		return nil, user.ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) ListUsers(context.Context, ...repository.ListOption) ([]*entity.User, error) {
	return nil, nil
}

func (r *fakeUserRepo) CreateUser(_ context.Context, u *entity.User) (*entity.User, error) {
	u.ID = r.nextID
	r.nextID++
	r.byID[u.ID] = u
	r.byName[u.UserName] = u
	if u.Email != "" {
		r.byEmail[u.Email] = u
	}
	if u.PhoneNumber != "" {
		r.byPhone[u.PhoneNumber] = u
	}
	r.created = append(r.created, u)
	return u, nil
}

func (r *fakeUserRepo) UpdateUser(_ context.Context, u *entity.User) (*entity.UserUpdate, error) {
	previous := r.byID[u.ID]
	r.byID[u.ID] = u
	return &entity.UserUpdate{User: u, Previous: previous}, nil
}

func (r *fakeUserRepo) DeleteUser(_ context.Context, id int64, actorID int64) ([]int64, error) {
	r.deleted = append(r.deleted, id)
	r.actors = append(r.actors, actorID)
	return []int64{id}, nil
}

func TestCreate_UserNameConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byName["bob"] = &entity.User{ID: 7, UserName: "bob"}
	svc := NewUserService(repo)

	_, err := svc.Create(context.Background(), &entity.User{UserName: "bob"})
	if !errors.Is(err, user.ErrUserNameConflict) {
		t.Fatalf("expected ErrUserNameConflict, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("a conflicting create must not persist, got %d rows", len(repo.created))
	}
}

func TestCreate_EmailConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byEmail["a@b.com"] = &entity.User{ID: 7, UserName: "someone", Email: "a@b.com"}
	svc := NewUserService(repo)

	_, err := svc.Create(context.Background(), &entity.User{UserName: "bob", Email: "a@b.com"})
	if !errors.Is(err, user.ErrEmailConflict) {
		t.Fatalf("expected ErrEmailConflict, got %v", err)
	}
}

func TestCreate_PhoneConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byPhone["13800000000"] = &entity.User{ID: 7, UserName: "someone", PhoneNumber: "13800000000"}
	svc := NewUserService(repo)

	_, err := svc.Create(context.Background(), &entity.User{UserName: "bob", PhoneNumber: "13800000000"})
	if !errors.Is(err, user.ErrPhoneConflict) {
		t.Fatalf("expected ErrPhoneConflict, got %v", err)
	}
}

func TestCreate_EmptyEmailAndPhoneNotChecked(t *testing.T) {
	// A user with no email/phone must not trip those uniqueness checks.
	svc := NewUserService(newFakeUserRepo())

	if _, err := svc.Create(context.Background(), &entity.User{UserName: "bob"}); err != nil {
		t.Fatalf("create without email and phone: %v", err)
	}
}

func TestCreate_PersistsAndReturnsCreatedUser(t *testing.T) {
	repo := newFakeUserRepo()
	svc := NewUserService(repo)

	created, err := svc.Create(context.Background(), &entity.User{UserName: "bob", Email: "a@b.com", PhoneNumber: "13800000000"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if len(repo.created) != 1 || repo.created[0] != created {
		t.Fatalf("persisted users = %#v, want exactly the returned user", repo.created)
	}
	if created.ID == 0 {
		t.Fatal("expected the repository-assigned id on the returned user")
	}
}

func TestUpdate_UserNameConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[2] = &entity.User{ID: 2, UserName: "old"}
	repo.byName["bob"] = &entity.User{ID: 7, UserName: "bob"}
	svc := NewUserService(repo)

	_, err := svc.Update(context.Background(), &entity.User{ID: 2, UserName: "bob"})
	if !errors.Is(err, user.ErrUserNameConflict) {
		t.Fatalf("expected ErrUserNameConflict, got %v", err)
	}
}

func TestUpdate_EmailConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[2] = &entity.User{ID: 2, UserName: "bob"}
	repo.byEmail["a@b.com"] = &entity.User{ID: 7, UserName: "someone", Email: "a@b.com"}
	svc := NewUserService(repo)

	_, err := svc.Update(context.Background(), &entity.User{ID: 2, UserName: "bob", Email: "a@b.com"})
	if !errors.Is(err, user.ErrEmailConflict) {
		t.Fatalf("expected ErrEmailConflict, got %v", err)
	}
}

func TestUpdate_SameValuesPass(t *testing.T) {
	// Updating a record while keeping its own unique values must pass: the
	// check excludes the record being updated.
	repo := newFakeUserRepo()
	repo.byID[5] = &entity.User{ID: 5, UserName: "alice", Email: "a@b.com", PhoneNumber: "13800000000"}
	repo.byName["alice"] = repo.byID[5]
	repo.byEmail["a@b.com"] = repo.byID[5]
	repo.byPhone["13800000000"] = repo.byID[5]
	svc := NewUserService(repo)

	if _, err := svc.Update(context.Background(), &entity.User{ID: 5, UserName: "alice", Email: "a@b.com", PhoneNumber: "13800000000", NickName: "renamed"}); err != nil {
		t.Fatalf("update keeping own values: %v", err)
	}
}

func TestUpdate_EmptyEmailNotChecked(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[1] = &entity.User{ID: 1, UserName: "bob"}
	svc := NewUserService(repo)

	if _, err := svc.Update(context.Background(), &entity.User{ID: 1, UserName: "bob"}); err != nil {
		t.Fatalf("update without email: %v", err)
	}
}

func TestUpdate_ReturnsWriteWithPreImage(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[1] = &entity.User{ID: 1, UserName: "bob", NickName: "old"}
	svc := NewUserService(repo)

	result, err := svc.Update(context.Background(), &entity.User{ID: 1, UserName: "bob", NickName: "new"})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if result.User.NickName != "new" || result.Previous.NickName != "old" {
		t.Fatalf("write = %#v, want the persisted change with its pre-image", result)
	}
}

func TestDelete_DelegatesWithActorAndReturnsAffectedIDs(t *testing.T) {
	repo := newFakeUserRepo()
	svc := NewUserService(repo)

	affected, err := svc.Delete(context.Background(), 42, 7)
	if err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 42 || repo.actors[0] != 7 {
		t.Fatalf("repo deletes = %v by %v, want user 42 by actor 7", repo.deleted, repo.actors)
	}
	if len(affected) != 1 || affected[0] != 42 {
		t.Fatalf("affected ids = %v, want [42] (sessions to revoke)", affected)
	}
}
