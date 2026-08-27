package user

import (
	authnmodel "glex/internal/crossdomain/authn/model"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	. "glex/internal/domain/user"
	userentity "glex/internal/domain/user/entity"
	userrepo "glex/internal/domain/user/repository"

	userv1 "glex/api/user/v1"

	"context"
	"testing"

	"github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeCredentialWriter records password requests handed to authn's credential
// capability; the user module never hashes or stores the credential itself.
type fakeCredentialWriter struct {
	written []*authnmodel.SystemUserCredential
	err     error
}

func (w *fakeCredentialWriter) WriteSystemUserCredential(_ context.Context, request *authnmodel.SystemUserCredential) error {
	snapshot := *request
	w.written = append(w.written, &snapshot)
	return w.err
}

// fakeUserRepo is an in-memory UserRepo for testing the application service in
// isolation from the infrastructure layer.
type fakeUserRepo struct {
	byID        map[int64]*userentity.User
	byName      map[string]*userentity.User
	byEmail     map[string]*userentity.User
	byPhone     map[string]*userentity.User
	userNameErr error
	nextID      int64
	created     []*userentity.User
	updated     []userentity.User
	deleted     []int64
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byID:    make(map[int64]*userentity.User),
		byName:  make(map[string]*userentity.User),
		byEmail: make(map[string]*userentity.User),
		byPhone: make(map[string]*userentity.User),
		nextID:  1,
	}
}

func (r *fakeUserRepo) FindByID(_ context.Context, id int64) (*userentity.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByUserName(_ context.Context, name string) (*userentity.User, error) {
	if r.userNameErr != nil {
		return nil, r.userNameErr
	}
	u, ok := r.byName[name]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (*userentity.User, error) {
	u, ok := r.byEmail[email]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByPhoneNumber(_ context.Context, phone string) (*userentity.User, error) {
	u, ok := r.byPhone[phone]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) ListUsers(context.Context, ...userrepo.ListOption) ([]*userentity.User, error) {
	return nil, nil
}

func (r *fakeUserRepo) CreateUser(_ context.Context, u *userentity.User) (*userentity.User, error) {
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

func (r *fakeUserRepo) UpdateUser(_ context.Context, u *userentity.User) (*userrepo.UserUpdate, error) {
	previous := r.byID[u.ID]
	r.byID[u.ID] = u
	r.updated = append(r.updated, *u)
	return &userrepo.UserUpdate{User: u, Previous: previous}, nil
}

func (r *fakeUserRepo) DeleteUser(_ context.Context, id int64, _ int64) ([]int64, error) {
	r.deleted = append(r.deleted, id)
	return []int64{id}, nil
}

func newTestUserApplication(repo *fakeUserRepo, credentials *fakeCredentialWriter, revoker *fakeSessionRevoker) *UserApplication {
	return NewUserApplication(repo, credentials, newFakeTransaction(), revoker, testLogger())
}

func createUserRequest(u *userv1.User) *userv1.CreateUserRequest {
	return &userv1.CreateUserRequest{User: u}
}

func updateUserRequest(u *userv1.User) *userv1.UpdateUserRequest {
	return &userv1.UpdateUserRequest{User: u}
}

func TestCreateUserDelegatesCredentialToAuthn(t *testing.T) {
	repo := newFakeUserRepo()
	credentials := new(fakeCredentialWriter)
	app := newTestUserApplication(repo, credentials, new(fakeSessionRevoker))

	_, err := app.CreateUser(context.Background(), createUserRequest(&userv1.User{UserName: "bob", NickName: "Bob", Password: "secret"}))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created user, got %d", len(repo.created))
	}
	if len(credentials.written) != 1 || credentials.written[0].SystemUserID != repo.created[0].ID || credentials.written[0].Password != "secret" {
		t.Fatalf("credential writes = %#v, want one write of the created user's plaintext", credentials.written)
	}
}

func TestCreateUser_PasswordRequired(t *testing.T) {
	app := newTestUserApplication(newFakeUserRepo(), new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.CreateUser(context.Background(), createUserRequest(&userv1.User{UserName: "bob", Password: ""}))
	if err == nil {
		t.Fatal("expected error for missing password, got nil")
	}
	if !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("expected ErrPasswordRequired, got %v", err)
	}
}

func TestCreateUser_UserNameConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byName["bob"] = &userentity.User{ID: 7, UserName: "bob"}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.CreateUser(context.Background(), createUserRequest(&userv1.User{UserName: "bob", Password: "secret"}))
	if !errors.Is(err, ErrUserNameConflict) {
		t.Fatalf("expected ErrUserNameConflict, got %v", err)
	}
}

func TestCreateUser_EmailConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byEmail["a@b.com"] = &userentity.User{ID: 7, UserName: "someone", Email: "a@b.com"}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.CreateUser(context.Background(), createUserRequest(&userv1.User{UserName: "bob", Email: "a@b.com", Password: "secret"}))
	if !errors.Is(err, ErrEmailConflict) {
		t.Fatalf("expected ErrEmailConflict, got %v", err)
	}
}

func TestCreateUser_EmptyEmailNotChecked(t *testing.T) {
	// A user with no email must not trip the email-uniqueness check.
	app := newTestUserApplication(newFakeUserRepo(), new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.CreateUser(context.Background(), createUserRequest(&userv1.User{UserName: "bob", Password: "secret"}))
	if err != nil {
		t.Fatalf("create user without email: %v", err)
	}
}

func TestUpdateUser_EmptyPasswordLeftUnchanged(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[1] = &userentity.User{ID: 1, UserName: "bob"}
	credentials := new(fakeCredentialWriter)
	app := newTestUserApplication(repo, credentials, new(fakeSessionRevoker))

	_, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 1, UserName: "bob"}))
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if len(credentials.written) != 0 {
		t.Fatalf("empty password must not write a credential, got %#v", credentials.written)
	}
}

func TestUpdateUserDelegatesNewCredentialToAuthn(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[1] = &userentity.User{ID: 1, UserName: "bob"}
	credentials := new(fakeCredentialWriter)
	app := newTestUserApplication(repo, credentials, new(fakeSessionRevoker))

	_, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 1, UserName: "bob", Password: "newsecret"}))
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if len(credentials.written) != 1 || credentials.written[0].SystemUserID != 1 || credentials.written[0].Password != "newsecret" {
		t.Fatalf("credential writes = %#v, want one write of user 1's plaintext", credentials.written)
	}
}

func TestUpdateUserSelfPasswordPreservesOnlyCurrentSession(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[42] = &userentity.User{ID: 42, UserName: "alice", Password: "old-hash"}
	revoker := &fakeSessionRevoker{}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), revoker)
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(42, "alice", "current-session", nil, nil))

	if _, err := app.UpdateUser(ctx, updateUserRequest(&userv1.User{UserId: 42, UserName: "alice", Password: "newsecret"})); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if len(revoker.requests) != 1 || len(revoker.requests[0].SystemUserIDs) != 1 {
		t.Fatalf("revocation requests = %#v, want one single-user request", revoker.requests)
	}
	request := revoker.requests[0]
	if request.SystemUserIDs[0] != 42 || request.PreservedSessionID != "current-session" {
		t.Fatalf("request = %#v, want {42, preserve current-session}", request)
	}
}

func TestUpdateUserAdministrativePasswordResetRevokesAllSessions(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[42] = &userentity.User{ID: 42, UserName: "alice", Password: "old-hash"}
	revoker := &fakeSessionRevoker{}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), revoker)
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(7, "admin", "admin-session", nil, nil))

	if _, err := app.UpdateUser(ctx, updateUserRequest(&userv1.User{UserId: 42, UserName: "alice", Password: "reset-secret"})); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if len(revoker.requests) != 1 || len(revoker.requests[0].SystemUserIDs) != 1 {
		t.Fatalf("revocation requests = %#v, want one single-user request", revoker.requests)
	}
	request := revoker.requests[0]
	if request.SystemUserIDs[0] != 42 || request.PreservedSessionID != "" {
		t.Fatalf("request = %#v, want {42, revoke-all}", request)
	}
}

func TestUpdateUserProfileOnlyChangeSkipsRevocation(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[42] = &userentity.User{ID: 42, UserName: "alice", NickName: "old"}
	revoker := &fakeSessionRevoker{}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), revoker)

	if _, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 42, UserName: "alice", NickName: "new"})); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if len(revoker.requests) != 0 {
		t.Fatalf("profile-only update must not revoke, got %#v", revoker.requests)
	}
}

func TestUpdateUserRevocationFailureFailsTheWrite(t *testing.T) {
	// The revocation is the last step inside the closure; its failure must
	// surface so the transaction rolls the mutation back.
	repo := newFakeUserRepo()
	repo.byID[42] = &userentity.User{ID: 42, UserName: "alice", Password: "old-hash"}
	revoker := &fakeSessionRevoker{err: authndomain.ErrAuthBackendUnavailable, failOnRequest: 1}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), revoker)

	if _, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 42, UserName: "alice", Password: "reset"})); err == nil {
		t.Fatal("revocation failure must fail UpdateUser")
	}
}

func TestUpdateUser_UserNameConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[2] = &userentity.User{ID: 2, UserName: "old"}
	repo.byName["bob"] = &userentity.User{ID: 7, UserName: "bob"}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 2, UserName: "bob"}))
	if !errors.Is(err, ErrUserNameConflict) {
		t.Fatalf("expected ErrUserNameConflict, got %v", err)
	}
}

func TestUpdateUser_SameValuesPass(t *testing.T) {
	// Updating a record while keeping its own unique values must pass: the
	// check excludes the record being updated.
	repo := newFakeUserRepo()
	repo.byID[5] = &userentity.User{ID: 5, UserName: "alice"}
	repo.byName["alice"] = repo.byID[5]
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 5, UserName: "alice", Email: "a@b.com"}))
	if err != nil {
		t.Fatalf("update keeping own values: %v", err)
	}
}

func TestUpdateUser_EmailConflict(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[2] = &userentity.User{ID: 2, UserName: "bob"}
	repo.byEmail["a@b.com"] = &userentity.User{ID: 7, UserName: "someone", Email: "a@b.com"}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 2, UserName: "bob", Email: "a@b.com"}))
	if !errors.Is(err, ErrEmailConflict) {
		t.Fatalf("expected ErrEmailConflict, got %v", err)
	}
}

func TestUpdateUser_EmptyEmailNotChecked(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[1] = &userentity.User{ID: 1, UserName: "bob"}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 1, UserName: "bob"}))
	if err != nil {
		t.Fatalf("update without email: %v", err)
	}
}

func TestGetInfoReadsUserAndProjectsPrincipalSnapshot(t *testing.T) {
	repo := newFakeUserRepo()
	repo.byID[42] = &userentity.User{ID: 42, UserName: "alice"}
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(42, "alice", "session-id", []string{"operator"}, []string{"system:user:list"}))

	info, err := app.GetInfo(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if info.GetUser().GetUserId() != 42 || info.GetUser().GetUserName() != "alice" {
		t.Fatalf("current System User = %#v", info.GetUser())
	}
	if len(info.GetRoleKeys()) != 1 || info.GetRoleKeys()[0] != "operator" || len(info.GetPermissions()) != 1 || info.GetPermissions()[0] != "system:user:list" {
		t.Fatalf("snapshot projection = Roles %v, Functional Permissions %v", info.GetRoleKeys(), info.GetPermissions())
	}
}

func TestGetInfoRequiresPrincipal(t *testing.T) {
	app := newTestUserApplication(newFakeUserRepo(), new(fakeCredentialWriter), new(fakeSessionRevoker))
	if _, err := app.GetInfo(t.Context(), &emptypb.Empty{}); !errors.Is(err, authndomain.ErrCredentialMissing) {
		t.Fatalf("GetInfo without Principal = %v", err)
	}
}

func TestDeleteSystemUserDelegatesSecurityTrigger(t *testing.T) {
	repo := newFakeUserRepo()
	app := newTestUserApplication(repo, new(fakeCredentialWriter), new(fakeSessionRevoker))

	if _, err := app.DeleteUser(t.Context(), &userv1.DeleteUserRequest{UserId: 42}); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 42 {
		t.Fatalf("deleted System Users = %v, want [42]", repo.deleted)
	}
}
