package biz

import (
	"context"
	"testing"

	"github.com/go-kratos/kratos/v3/errors"
	"golang.org/x/crypto/bcrypt"
)

// fakeUserRepo is an in-memory UserRepo for testing the usecase in isolation
// from the data layer.
type fakeUserRepo struct {
	byID           map[int64]*User
	byName         map[string]*User
	byEmail        map[string]*User
	byPhone        map[string]*User
	nextID         int64
	created        []*User
	passwordPolicy SessionRevocation
	deleted        []int64
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byID:    make(map[int64]*User),
		byName:  make(map[string]*User),
		byEmail: make(map[string]*User),
		byPhone: make(map[string]*User),
		nextID:  1,
	}
}

func (r *fakeUserRepo) FindByID(_ context.Context, id int64) (*User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByUserName(_ context.Context, name string) (*User, error) {
	u, ok := r.byName[name]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (*User, error) {
	u, ok := r.byEmail[email]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByPhoneNumber(_ context.Context, phone string) (*User, error) {
	u, ok := r.byPhone[phone]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) ListUsers(context.Context, ...ListOption) ([]*User, error) {
	return nil, nil
}

func (r *fakeUserRepo) CreateUser(_ context.Context, u *User) (*User, error) {
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

func (r *fakeUserRepo) UpdateUser(_ context.Context, u *User, passwordPolicy SessionRevocation) (*User, error) {
	r.passwordPolicy = passwordPolicy
	r.byID[u.ID] = u
	return u, nil
}

func (r *fakeUserRepo) DeleteUser(_ context.Context, id int64) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func TestCreateUser_HashesPassword(t *testing.T) {
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)

	_, err := uc.CreateUser(context.Background(), &User{UserName: "bob", NickName: "Bob", Password: "secret"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created user, got %d", len(repo.created))
	}
	stored := repo.created[0].Password
	if stored == "secret" {
		t.Fatal("password was stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte("secret")); err != nil {
		t.Fatalf("stored password is not a valid bcrypt hash of the input: %v", err)
	}
}

func TestCreateUser_PasswordRequired(t *testing.T) {
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)

	_, err := uc.CreateUser(context.Background(), &User{UserName: "bob", Password: ""})
	if err == nil {
		t.Fatal("expected error for missing password, got nil")
	}
	if !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("expected ErrPasswordRequired, got %v", err)
	}
}

func TestCreateUser_UserNameConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byName["bob"] = &User{ID: 7, UserName: "bob"}
	uc := NewUserUsecase(repo)

	_, err := uc.CreateUser(context.Background(), &User{UserName: "bob", Password: "secret"})
	if !errors.Is(err, ErrUserNameConflict) {
		t.Fatalf("expected ErrUserNameConflict, got %v", err)
	}
}

func TestCreateUser_EmailConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byEmail["a@b.com"] = &User{ID: 7, UserName: "someone", Email: "a@b.com"}
	uc := NewUserUsecase(repo)

	_, err := uc.CreateUser(context.Background(), &User{UserName: "bob", Email: "a@b.com", Password: "secret"})
	if !errors.Is(err, ErrEmailConflict) {
		t.Fatalf("expected ErrEmailConflict, got %v", err)
	}
}

func TestCreateUser_EmptyEmailNotChecked(t *testing.T) {
	// A user with no email must not trip the email-uniqueness check.
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)

	_, err := uc.CreateUser(context.Background(), &User{UserName: "bob", Password: "secret"})
	if err != nil {
		t.Fatalf("create user without email: %v", err)
	}
}

func TestUpdateUser_EmptyPasswordLeftUnchanged(t *testing.T) {
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)

	got, err := uc.UpdateUser(context.Background(), &User{ID: 1, UserName: "bob", Password: ""})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if got.Password != "" {
		t.Fatalf("empty password must be passed through unchanged, got %q", got.Password)
	}
}

func TestUpdateUser_HashesNewPassword(t *testing.T) {
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)

	got, err := uc.UpdateUser(context.Background(), &User{ID: 1, UserName: "bob", Password: "newsecret"})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if got.Password == "newsecret" {
		t.Fatal("new password was stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(got.Password), []byte("newsecret")); err != nil {
		t.Fatalf("updated password is not a valid bcrypt hash: %v", err)
	}
}

func TestUpdateUserSelfPasswordPreservesOnlyCurrentSession(t *testing.T) {
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)
	ctx := WithPrincipal(t.Context(), NewPrincipal(42, "alice", "current-session", nil, nil))

	if _, err := uc.UpdateUser(ctx, &User{ID: 42, UserName: "alice", Password: "newsecret"}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if repo.passwordPolicy.PreservedSessionID() != "current-session" {
		t.Fatalf("preserved Session = %q, want current-session", repo.passwordPolicy.PreservedSessionID())
	}
}

func TestUpdateUserAdministrativePasswordResetRevokesAllSessions(t *testing.T) {
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)
	ctx := WithPrincipal(t.Context(), NewPrincipal(7, "admin", "admin-session", nil, nil))

	if _, err := uc.UpdateUser(ctx, &User{ID: 42, UserName: "alice", Password: "reset-secret"}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if repo.passwordPolicy.PreservedSessionID() != "" || !repo.passwordPolicy.Required() {
		t.Fatalf("administrative reset policy = %#v", repo.passwordPolicy)
	}
}

func TestUpdateUser_UserNameConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byName["bob"] = &User{ID: 7, UserName: "bob"}
	uc := NewUserUsecase(repo)

	_, err := uc.UpdateUser(context.Background(), &User{ID: 2, UserName: "bob"})
	if !errors.Is(err, ErrUserNameConflict) {
		t.Fatalf("expected ErrUserNameConflict, got %v", err)
	}
}

func TestUpdateUser_SameValuesPass(t *testing.T) {
	// Updating a record while keeping its own unique values must pass: the
	// check excludes the record being updated.
	repo := newFakeUserRepo()
	repo.byID[5] = &User{ID: 5, UserName: "alice"}
	repo.byName["alice"] = repo.byID[5]
	uc := NewUserUsecase(repo)

	_, err := uc.UpdateUser(context.Background(), &User{ID: 5, UserName: "alice", Email: "a@b.com"})
	if err != nil {
		t.Fatalf("update keeping own values: %v", err)
	}
}

func TestUpdateUser_EmailConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byEmail["a@b.com"] = &User{ID: 7, UserName: "someone", Email: "a@b.com"}
	uc := NewUserUsecase(repo)

	_, err := uc.UpdateUser(context.Background(), &User{ID: 2, UserName: "bob", Email: "a@b.com"})
	if !errors.Is(err, ErrEmailConflict) {
		t.Fatalf("expected ErrEmailConflict, got %v", err)
	}
}

func TestUpdateUser_EmptyEmailNotChecked(t *testing.T) {
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)

	_, err := uc.UpdateUser(context.Background(), &User{ID: 1, UserName: "bob"})
	if err != nil {
		t.Fatalf("update without email: %v", err)
	}
}

func TestGetCurrentUserInfoReadsUserAndCopiesPrincipalSnapshot(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[42] = &User{ID: 42, UserName: "alice", Password: "stored-hash"}
	uc := NewUserUsecase(repo)
	roleKeys := []string{"operator"}
	permissions := []string{"system:user:list"}
	principal := NewPrincipal(42, "alice", "session-id", roleKeys, permissions)

	info, err := uc.GetCurrentUserInfo(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetCurrentUserInfo: %v", err)
	}
	if info.User != repo.byID[42] || info.User.ID != 42 {
		t.Fatalf("current System User = %#v", info.User)
	}
	roleKeys[0] = "mutated"
	permissions[0] = "mutated"
	if info.RoleKeys[0] != "operator" || info.Permissions[0] != "system:user:list" {
		t.Fatalf("snapshot was not defensively copied: %#v", info)
	}
}

func TestGetCurrentUserInfoRequiresPrincipal(t *testing.T) {
	uc := NewUserUsecase(newFakeUserRepo())
	if _, err := uc.GetCurrentUserInfo(t.Context(), nil); !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("GetCurrentUserInfo(nil) = %v", err)
	}
}

func TestDeleteSystemUserDelegatesSecurityTrigger(t *testing.T) {
	repo := newFakeUserRepo()
	uc := NewUserUsecase(repo)

	if err := uc.DeleteUser(t.Context(), 42); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 42 {
		t.Fatalf("deleted System Users = %v, want [42]", repo.deleted)
	}
}
