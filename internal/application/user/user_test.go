package user

import (
	"context"
	"strconv"
	"testing"
	"time"

	"glex/internal/conf"

	authnmodel "glex/internal/crossdomain/authn/model"
	authndomain "glex/internal/domain/authn"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	orgdomain "glex/internal/domain/org"
	userdomain "glex/internal/domain/user"
	userentity "glex/internal/domain/user/entity"
	userservice "glex/internal/domain/user/service"

	userv1 "glex/api/user/v1"

	"github.com/go-kratos/kratos/v3/errors"
	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fakeURLResolver is the file crossdomain contract double: it hands out a
// canned Display URL per asked id without touching the file module.
type fakeURLResolver struct{}

func (f *fakeURLResolver) ResolveURLs(_ context.Context, fileIDs []int64) (map[int64]string, error) {
	urls := make(map[int64]string, len(fileIDs))
	for _, id := range fileIDs {
		urls[id] = "https://signed.example/" + strconv.FormatInt(id, 10)
	}
	return urls, nil
}

// fakeCredentialWriter records password requests handed to authn's credential
// capability; the user module never hashes or stores the credential itself.
type fakeCredentialWriter struct {
	written []*authnmodel.SystemUserCredential
	err     error
	steps   *[]string
}

func (w *fakeCredentialWriter) WriteSystemUserCredential(_ context.Context, request *authnmodel.SystemUserCredential) error {
	snapshot := *request
	w.written = append(w.written, &snapshot)
	recordStep(w.steps, "credentials.write")
	return w.err
}

// WriteSystemUserCredentials records the Data Import batch ask (ADR 0017) —
// one shared password, ids only.
func (w *fakeCredentialWriter) WriteSystemUserCredentials(_ context.Context, systemUserIDs []int64, password string) error {
	for _, id := range systemUserIDs {
		w.written = append(w.written, &authnmodel.SystemUserCredential{SystemUserID: id, Password: password})
	}
	return w.err
}

// fakeUserService is an in-memory userservice.User for testing the application
// orchestration in isolation from the domain: it carries no domain rules (the
// uniqueness checks live in the service's own tests), just enough state to
// observe what the application hands over and returns across its seams.
type fakeUserService struct {
	byID   map[int64]*userentity.User
	nextID int64
	// list backs List with offset/limit windowing so pagination flows through
	// the real page-token mechanics.
	list []*userentity.User
	// conflictOn makes Create refuse that login account with the domain's
	// uniqueness sentinel — Data Import's in-transaction conflict path.
	conflictOn string

	created      []*userentity.User
	updated      []userentity.User
	deletedIDs   []int64
	deletedBy    []int64
	lastListCall struct {
		limit  int
		offset int
	}
	steps *[]string
}

func newFakeUserService() *fakeUserService {
	return &fakeUserService{
		byID:   make(map[int64]*userentity.User),
		nextID: 1,
	}
}

var _ userservice.User = (*fakeUserService)(nil)

func (s *fakeUserService) Get(_ context.Context, id int64) (*userentity.User, error) {
	u, ok := s.byID[id]
	if !ok {
		return nil, userdomain.ErrUserNotFound
	}
	return u, nil
}

func (s *fakeUserService) List(_ context.Context, _ filtering.Filter, _ ordering.OrderBy, limit, offset int, _ []int64) ([]*userentity.User, error) {
	s.lastListCall.limit = limit
	s.lastListCall.offset = offset
	if offset >= len(s.list) {
		return nil, nil
	}
	end := len(s.list)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return append([]*userentity.User(nil), s.list[offset:end]...), nil
}

func (s *fakeUserService) Create(_ context.Context, u *userentity.User) (*userentity.User, error) {
	if s.conflictOn != "" && u.UserName == s.conflictOn {
		return nil, userdomain.ErrUserNameConflict
	}
	u.ID = s.nextID
	s.nextID++
	s.byID[u.ID] = u
	s.created = append(s.created, u)
	recordStep(s.steps, "svc.create")
	return u, nil
}

func (s *fakeUserService) Update(_ context.Context, u *userentity.User) (*userentity.UserUpdate, error) {
	previous := s.byID[u.ID]
	s.byID[u.ID] = u
	s.updated = append(s.updated, *u)
	return &userentity.UserUpdate{User: u, Previous: previous}, nil
}

func (s *fakeUserService) Delete(_ context.Context, id int64, actorID int64) ([]int64, error) {
	s.deletedIDs = append(s.deletedIDs, id)
	s.deletedBy = append(s.deletedBy, actorID)
	delete(s.byID, id)
	return []int64{id}, nil
}

func newTestUserApplication(svc *fakeUserService, credentials *fakeCredentialWriter, revoker *fakeSessionRevoker) *UserApplication {
	return NewUserApplication(svc, credentials, new(fakeDeptReader), new(fakeURLResolver), nil, nil, new(conf.User), newFakeTransaction(), revoker, testLogger())
}

func createUserRequest(u *userv1.User) *userv1.CreateUserRequest {
	return &userv1.CreateUserRequest{User: u}
}

func updateUserRequest(u *userv1.User) *userv1.UpdateUserRequest {
	return &userv1.UpdateUserRequest{User: u}
}

func TestCreateUserDelegatesCredentialToAuthn(t *testing.T) {
	svc := newFakeUserService()
	credentials := new(fakeCredentialWriter)
	app := newTestUserApplication(svc, credentials, new(fakeSessionRevoker))

	_, err := app.CreateUser(context.Background(), createUserRequest(&userv1.User{UserName: "bob", NickName: "Bob", Password: "secret"}))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if len(svc.created) != 1 {
		t.Fatalf("expected 1 created user, got %d", len(svc.created))
	}
	if len(credentials.written) != 1 || credentials.written[0].SystemUserID != svc.created[0].ID || credentials.written[0].Password != "secret" {
		t.Fatalf("credential writes = %#v, want one write of the created user's plaintext", credentials.written)
	}
}

func TestCreateUserRunsCreateAndCredentialWriteInOneTransaction(t *testing.T) {
	tx := newFakeTransaction()
	steps := tx.step()
	svc := newFakeUserService()
	svc.steps = steps
	credentials := &fakeCredentialWriter{steps: steps}
	app := NewUserApplication(svc, credentials, new(fakeDeptReader), new(fakeURLResolver), nil, nil, new(conf.User), tx, new(fakeSessionRevoker), testLogger())

	if _, err := app.CreateUser(context.Background(), createUserRequest(&userv1.User{UserName: "bob", Password: "secret"})); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if tx.began != 1 {
		t.Fatalf("InTx began %d times, want exactly one transaction", tx.began)
	}
	assertSteps(t, steps, "svc.create", "credentials.write")
}

func TestCreateUser_PasswordRequired(t *testing.T) {
	// The password-required guard runs in the application before any service
	// call, so a missing password leaves the service untouched.
	svc := newFakeUserService()
	app := newTestUserApplication(svc, new(fakeCredentialWriter), new(fakeSessionRevoker))

	_, err := app.CreateUser(context.Background(), createUserRequest(&userv1.User{UserName: "bob", Password: ""}))
	if !errors.Is(err, userdomain.ErrPasswordRequired) {
		t.Fatalf("expected ErrPasswordRequired, got %v", err)
	}
	if len(svc.created) != 0 {
		t.Fatalf("missing password must not reach the service, got %d creates", len(svc.created))
	}
}

func TestUpdateUserAppliesUpdateMask(t *testing.T) {
	// Fields outside the mask are ignored: only nick_name changes, and the
	// masked-out password means "leave unchanged".
	svc := newFakeUserService()
	svc.byID[5] = &userentity.User{ID: 5, UserName: "alice", NickName: "old"}
	credentials := new(fakeCredentialWriter)
	app := newTestUserApplication(svc, credentials, new(fakeSessionRevoker))

	updated, err := app.UpdateUser(context.Background(), &userv1.UpdateUserRequest{
		User:       &userv1.User{UserId: 5, UserName: "mallory", NickName: "new", Password: "newsecret"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"nick_name"}},
	})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if len(svc.updated) != 1 || svc.updated[0].UserName != "alice" || svc.updated[0].NickName != "new" {
		t.Fatalf("persisted change = %#v, want user_name unchanged and nick_name updated", svc.updated)
	}
	if updated.GetUserName() != "alice" || updated.GetNickName() != "new" {
		t.Fatalf("returned user = %#v, want the merged masked state", updated)
	}
	if len(credentials.written) != 0 {
		t.Fatalf("a password outside the mask must not write a credential, got %#v", credentials.written)
	}
}

func TestUpdateUser_EmptyPasswordLeftUnchanged(t *testing.T) {
	svc := newFakeUserService()
	svc.byID[1] = &userentity.User{ID: 1, UserName: "bob"}
	credentials := new(fakeCredentialWriter)
	app := newTestUserApplication(svc, credentials, new(fakeSessionRevoker))

	_, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 1, UserName: "bob"}))
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if len(credentials.written) != 0 {
		t.Fatalf("empty password must not write a credential, got %#v", credentials.written)
	}
}

func TestUpdateUserDelegatesNewCredentialToAuthn(t *testing.T) {
	svc := newFakeUserService()
	svc.byID[1] = &userentity.User{ID: 1, UserName: "bob"}
	credentials := new(fakeCredentialWriter)
	app := newTestUserApplication(svc, credentials, new(fakeSessionRevoker))

	_, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 1, UserName: "bob", Password: "newsecret"}))
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if len(credentials.written) != 1 || credentials.written[0].SystemUserID != 1 || credentials.written[0].Password != "newsecret" {
		t.Fatalf("credential writes = %#v, want one write of user 1's plaintext", credentials.written)
	}
}

func TestUpdateUserSelfPasswordPreservesOnlyCurrentSession(t *testing.T) {
	svc := newFakeUserService()
	svc.byID[42] = &userentity.User{ID: 42, UserName: "alice"}
	revoker := &fakeSessionRevoker{}
	app := newTestUserApplication(svc, new(fakeCredentialWriter), revoker)
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
	svc := newFakeUserService()
	svc.byID[42] = &userentity.User{ID: 42, UserName: "alice"}
	revoker := &fakeSessionRevoker{}
	app := newTestUserApplication(svc, new(fakeCredentialWriter), revoker)
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
	svc := newFakeUserService()
	svc.byID[42] = &userentity.User{ID: 42, UserName: "alice", NickName: "old"}
	revoker := &fakeSessionRevoker{}
	app := newTestUserApplication(svc, new(fakeCredentialWriter), revoker)

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
	svc := newFakeUserService()
	svc.byID[42] = &userentity.User{ID: 42, UserName: "alice"}
	revoker := &fakeSessionRevoker{err: authndomain.ErrAuthBackendUnavailable, failOnRequest: 1}
	app := newTestUserApplication(svc, new(fakeCredentialWriter), revoker)

	if _, err := app.UpdateUser(context.Background(), updateUserRequest(&userv1.User{UserId: 42, UserName: "alice", Password: "reset"})); err == nil {
		t.Fatal("revocation failure must fail UpdateUser")
	}
}

func TestGetInfoReadsUserAndProjectsPrincipalSnapshot(t *testing.T) {
	svc := newFakeUserService()
	svc.byID[42] = &userentity.User{ID: 42, UserName: "alice"}
	app := newTestUserApplication(svc, new(fakeCredentialWriter), new(fakeSessionRevoker))
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
	app := newTestUserApplication(newFakeUserService(), new(fakeCredentialWriter), new(fakeSessionRevoker))
	if _, err := app.GetInfo(t.Context(), &emptypb.Empty{}); !errors.Is(err, authndomain.ErrCredentialMissing) {
		t.Fatalf("GetInfo without Principal = %v", err)
	}
}

func TestListUsersPaginatesWithPageTokens(t *testing.T) {
	svc := newFakeUserService()
	for i := 1; i <= 25; i++ {
		u := &userentity.User{ID: int64(i), UserName: "user"}
		svc.byID[u.ID] = u
		svc.list = append(svc.list, u)
	}
	app := newTestUserApplication(svc, new(fakeCredentialWriter), new(fakeSessionRevoker))

	// Page one: a full page carries a next-page token.
	page, err := app.ListUsers(context.Background(), &userv1.ListUsersRequest{PageSize: 10})
	if err != nil {
		t.Fatalf("list page one: %v", err)
	}
	if len(page.GetUsers()) != 10 || page.GetUsers()[0].GetUserId() != 1 || page.GetUsers()[9].GetUserId() != 10 {
		t.Fatalf("page one users = %#v, want ids 1-10", page.GetUsers())
	}
	if page.GetNextPageToken() == "" {
		t.Fatal("full page must carry a next-page token")
	}
	if svc.lastListCall.limit != 10 || svc.lastListCall.offset != 0 {
		t.Fatalf("service list window = limit %d offset %d, want 10/0", svc.lastListCall.limit, svc.lastListCall.offset)
	}

	// Page two: the opaque token resumes at the recorded offset.
	page, err = app.ListUsers(context.Background(), &userv1.ListUsersRequest{PageSize: 10, PageToken: page.GetNextPageToken()})
	if err != nil {
		t.Fatalf("list page two: %v", err)
	}
	if len(page.GetUsers()) != 10 || page.GetUsers()[0].GetUserId() != 11 || page.GetUsers()[9].GetUserId() != 20 {
		t.Fatalf("page two users = %#v, want ids 11-20", page.GetUsers())
	}
	if svc.lastListCall.offset != 10 {
		t.Fatalf("service list offset = %d, want 10", svc.lastListCall.offset)
	}
	if page.GetNextPageToken() == "" {
		t.Fatal("full page must carry a next-page token")
	}

	// Page three: a short final page has no token.
	page, err = app.ListUsers(context.Background(), &userv1.ListUsersRequest{PageSize: 10, PageToken: page.GetNextPageToken()})
	if err != nil {
		t.Fatalf("list page three: %v", err)
	}
	if len(page.GetUsers()) != 5 || page.GetUsers()[0].GetUserId() != 21 || page.GetUsers()[4].GetUserId() != 25 {
		t.Fatalf("page three users = %#v, want ids 21-25", page.GetUsers())
	}
	if page.GetNextPageToken() != "" {
		t.Fatalf("final partial page must not carry a token, got %q", page.GetNextPageToken())
	}
}

func TestListUsersDefaultsPageSizeToTwenty(t *testing.T) {
	svc := newFakeUserService()
	svc.list = []*userentity.User{{ID: 1, UserName: "user"}}
	app := newTestUserApplication(svc, new(fakeCredentialWriter), new(fakeSessionRevoker))

	if _, err := app.ListUsers(context.Background(), &userv1.ListUsersRequest{}); err != nil {
		t.Fatalf("list users: %v", err)
	}
	if svc.lastListCall.limit != 20 {
		t.Fatalf("service list limit = %d, want the default page size 20", svc.lastListCall.limit)
	}
}

func TestDeleteUserDelegatesDeleteAndRevocation(t *testing.T) {
	svc := newFakeUserService()
	revoker := &fakeSessionRevoker{}
	app := newTestUserApplication(svc, new(fakeCredentialWriter), revoker)
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(7, "admin", "admin-session", nil, nil))

	if _, err := app.DeleteUser(ctx, &userv1.DeleteUserRequest{UserId: 42}); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if len(svc.deletedIDs) != 1 || svc.deletedIDs[0] != 42 || svc.deletedBy[0] != 7 {
		t.Fatalf("service deletes = %v by %v, want user 42 by actor 7", svc.deletedIDs, svc.deletedBy)
	}
	if len(revoker.requests) != 1 || len(revoker.requests[0].SystemUserIDs) != 1 || revoker.requests[0].SystemUserIDs[0] != 42 || revoker.requests[0].PreservedSessionID != "" {
		t.Fatalf("revocation requests = %#v, want one revoke-all request for user 42", revoker.requests)
	}
}

func TestConvertUserOmitsPasswordAndMapsFields(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	loginDate := now.Add(time.Hour)
	dto := convertUser(&userentity.User{
		ID: 7, UserName: "alice", NickName: "Alice", Email: "a@b.com", PhoneNumber: "13800000000",
		Gender: 1, Password: "plaintext-secret", Status: 1, Avatar: 42, Remark: "r",
		CreateBy: 1, UpdateBy: 2, LoginIP: "127.0.0.1", LoginDate: &loginDate, CreatedAt: now, UpdatedAt: now,
	})

	if dto.GetUserId() != 7 || dto.GetUserName() != "alice" || dto.GetNickName() != "Alice" ||
		dto.GetEmail() != "a@b.com" || dto.GetPhoneNumber() != "13800000000" || dto.GetGender() != 1 ||
		dto.GetStatus() != 1 || dto.GetAvatar() != 42 || dto.GetRemark() != "r" ||
		dto.GetCreateBy() != 1 || dto.GetUpdateBy() != 2 || dto.GetLoginIp() != "127.0.0.1" {
		t.Fatalf("field mapping = %#v", dto)
	}
	if dto.GetPassword() != "" {
		t.Fatalf("password is write-only and must never be returned, got %q", dto.GetPassword())
	}
	if !dto.GetLoginDate().AsTime().Equal(loginDate) || !dto.GetCreatedAt().AsTime().Equal(now) || !dto.GetUpdatedAt().AsTime().Equal(now) {
		t.Fatalf("timestamp mapping = %#v", dto)
	}
}

func (f *fakeUserService) HasUsersInDept(_ context.Context, _ int64) (bool, error) {
	return false, nil
}

// fakeDeptReader is the org crossdomain contract double: it records the
// asks and returns the configured answer.
type fakeDeptReader struct {
	ensureID  int64
	ensureErr error
	subtree   []int64
	subtreeID int64
	paths     map[int64]string
	resolve   map[string]int64
}

func (f *fakeDeptReader) EnsureActiveDept(_ context.Context, deptID int64) error {
	f.ensureID = deptID
	return f.ensureErr
}

func (f *fakeDeptReader) ResolveSubtreeIDs(_ context.Context, rootDeptID int64) ([]int64, error) {
	f.subtreeID = rootDeptID
	return f.subtree, nil
}

func (f *fakeDeptReader) DeptPaths(_ context.Context, deptIDs []int64) (map[int64]string, error) {
	return f.paths, nil
}

func (f *fakeDeptReader) ResolveDeptPath(_ context.Context, path string) (int64, error) {
	if id, ok := f.resolve[path]; ok {
		return id, nil
	}
	return 0, orgdomain.ErrDeptNotFound
}

func TestCreateUser_ValidatesDeptThroughContract(t *testing.T) {
	svc := newFakeUserService()
	credentials := &fakeCredentialWriter{}
	depts := &fakeDeptReader{}
	app := NewUserApplication(svc, credentials, depts, new(fakeURLResolver), nil, nil, new(conf.User), newFakeTransaction(), new(fakeSessionRevoker), testLogger())
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(1, "alice", "session-id", nil, nil))

	if _, err := app.CreateUser(ctx, createUserRequest(&userv1.User{
		UserName: "bob", Password: "pw", DeptId: 3,
	})); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if depts.ensureID != 3 {
		t.Fatalf("ensureID = %d, want 3", depts.ensureID)
	}
	if svc.created[0].DeptID != 3 {
		t.Fatalf("DeptID = %d, want 3 persisted", svc.created[0].DeptID)
	}
}

func TestCreateUser_DeptValidationErrorAborts(t *testing.T) {
	svc := newFakeUserService()
	credentials := &fakeCredentialWriter{}
	depts := &fakeDeptReader{ensureErr: orgdomain.ErrDeptDisabled}
	app := NewUserApplication(svc, credentials, depts, new(fakeURLResolver), nil, nil, new(conf.User), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(1, "alice", "session-id", nil, nil))
	_, err := app.CreateUser(ctx, createUserRequest(&userv1.User{
		UserName: "bob", Password: "pw", DeptId: 3,
	}))
	if !errors.Is(err, orgdomain.ErrDeptDisabled) {
		t.Fatalf("err = %v, want org's ErrDeptDisabled verbatim", err)
	}
	if len(svc.created) != 0 || len(credentials.written) != 0 {
		t.Fatal("user or credential written despite dept validation failure")
	}
}

func TestCreateUser_ZeroDeptSkipsContract(t *testing.T) {
	svc := newFakeUserService()
	depts := &fakeDeptReader{}
	app := NewUserApplication(svc, &fakeCredentialWriter{}, depts, new(fakeURLResolver), nil, nil, new(conf.User), newFakeTransaction(), new(fakeSessionRevoker), testLogger())
	ctx := authnservice.WithPrincipal(t.Context(), authnentity.NewPrincipal(1, "alice", "session-id", nil, nil))

	if _, err := app.CreateUser(ctx, createUserRequest(&userv1.User{UserName: "bob", Password: "pw"})); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if depts.ensureID != 0 {
		t.Fatalf("ensureID = %d, want 0 (no ask)", depts.ensureID)
	}
}

func TestListUsers_ResolvesDeptSubtreeThroughContract(t *testing.T) {
	svc := newFakeUserService()
	svc.list = []*userentity.User{{ID: 7, UserName: "bob", DeptID: 3}}
	depts := &fakeDeptReader{subtree: []int64{2, 3}}
	app := NewUserApplication(svc, &fakeCredentialWriter{}, depts, new(fakeURLResolver), nil, nil, new(conf.User), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	set, err := app.ListUsers(context.Background(), &userv1.ListUsersRequest{DeptId: 2})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if depts.subtreeID != 2 {
		t.Fatalf("subtreeID = %d, want 2", depts.subtreeID)
	}
	if len(set.Users) != 1 || set.Users[0].DeptId != 3 {
		t.Fatalf("users = %+v", set.Users)
	}
}

func TestListUsers_ZeroDeptSkipsContract(t *testing.T) {
	svc := newFakeUserService()
	depts := &fakeDeptReader{}
	app := NewUserApplication(svc, &fakeCredentialWriter{}, depts, new(fakeURLResolver), nil, nil, new(conf.User), newFakeTransaction(), new(fakeSessionRevoker), testLogger())

	if _, err := app.ListUsers(context.Background(), &userv1.ListUsersRequest{}); err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if depts.subtreeID != 0 {
		t.Fatalf("subtreeID = %d, want 0 (no ask)", depts.subtreeID)
	}
}
