package param

import (
	"context"
	"errors"
	"testing"
	"time"

	paramv1 "glex/api/param/v1"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	paramentity "glex/internal/domain/param/entity"
	paramservice "glex/internal/domain/param/service"

	"glex/pkg/listq"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fakeConfigsService is a hand-rolled paramservice.Configs fake for the
// application tests: it records every call (including the ctx each write ran
// on) and returns the configured result, so the tests exercise orchestration
// only — transaction wrapping, DTO conversion, pagination, audit stamping —
// never the domain rules behind the interface.
type fakeConfigsService struct {
	// Configured returns.
	get       *paramentity.Config
	findByKey *paramentity.Config
	list      []*paramentity.Config
	create    *paramentity.Config
	update    *paramentity.Config
	getErr    error
	listErr   error
	createErr error
	updateErr error
	deleteErr error

	// Recorded calls.
	createCalled       int
	createCtx          context.Context
	createInput        *paramentity.Config
	listLimit          int
	listOffset         int
	listFilter         string
	updateCalled       int
	updateCtx          context.Context
	updateInput        *paramentity.Config
	deleteCalled       int
	deleteCtx          context.Context
	deleteID           int64
	deleteActor        int64
	findByKeyRequested string
}

func (f *fakeConfigsService) Get(_ context.Context, _ int64) (*paramentity.Config, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.get, nil
}

func (f *fakeConfigsService) FindByKey(_ context.Context, key string) (*paramentity.Config, error) {
	f.findByKeyRequested = key
	return f.findByKey, nil
}

func (f *fakeConfigsService) List(_ context.Context, q listq.Query) ([]*paramentity.Config, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.listLimit, f.listOffset, f.listFilter = q.Limit, q.Offset, q.Filter.CheckedExpr.String()
	return f.list, nil
}

func (f *fakeConfigsService) Create(ctx context.Context, c *paramentity.Config) (*paramentity.Config, error) {
	f.createCalled++
	f.createCtx, f.createInput = ctx, c
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.create, nil
}

func (f *fakeConfigsService) Update(ctx context.Context, c *paramentity.Config) (*paramentity.Config, error) {
	f.updateCalled++
	f.updateCtx, f.updateInput = ctx, c
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.update, nil
}

func (f *fakeConfigsService) Delete(ctx context.Context, req *paramservice.DeleteRequest) error {
	f.deleteCalled++
	f.deleteCtx, f.deleteID, f.deleteActor = ctx, req.ID, req.ActorID
	return f.deleteErr
}

// errServiceFailed is the propagation sentinel: application tests only assert
// that a service error crosses the handler untouched.
var errServiceFailed = errors.New("service failed")

// actorContext returns a ctx authenticating the given System User, so tests
// can assert the audit stamping.
func actorContext(userID int64) context.Context {
	return authnservice.WithPrincipal(context.Background(), authnentity.NewPrincipal(userID, "alice", "session-id", nil, nil))
}

func newParamApp(svc *fakeConfigsService) (*ConfigApplication, *fakeTransaction) {
	tx := newFakeTransaction()
	return NewConfigApplication(svc, tx, testLogger()), tx
}

func TestCreateConfig_StampsActorAndRunsInTx(t *testing.T) {
	svc := &fakeConfigsService{create: &paramentity.Config{ID: 7, ConfigName: "注册开关", ConfigKey: "sys.account.registerUser", ConfigValue: "false", BuiltIn: true, Remark: "r"}}
	app, tx := newParamApp(svc)

	got, err := app.CreateConfig(actorContext(42), &paramv1.CreateConfigRequest{
		Config: &paramv1.Config{ConfigName: "注册开关", ConfigKey: "sys.account.registerUser", ConfigValue: "false", BuiltIn: true, Remark: "r"},
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if svc.createCalled != 1 {
		t.Fatalf("expected exactly one service Create, got %d", svc.createCalled)
	}
	// The write runs on the ctx the transaction body received.
	if svc.createCtx != tx.bodyCtx {
		t.Fatal("service Create must see the ctx the transaction body received")
	}
	// The acting Principal is stamped into the audit fields.
	if svc.createInput.CreateBy != 42 || svc.createInput.UpdateBy != 42 {
		t.Fatalf("audit fields = %d/%d, want the acting principal 42", svc.createInput.CreateBy, svc.createInput.UpdateBy)
	}
	if !svc.createInput.BuiltIn {
		t.Fatal("built_in must survive the DTO→DO conversion")
	}
	if got.GetConfigId() != 7 || got.GetConfigKey() != "sys.account.registerUser" || !got.GetBuiltIn() {
		t.Fatalf("reply = %#v", got)
	}
}

func TestCreateConfig_SystemActorWhenUnauthenticated(t *testing.T) {
	svc := &fakeConfigsService{create: &paramentity.Config{}}
	app, _ := newParamApp(svc)

	if _, err := app.CreateConfig(context.Background(), &paramv1.CreateConfigRequest{Config: &paramv1.Config{ConfigName: "n", ConfigKey: "sys.k"}}); err != nil {
		t.Fatalf("create config: %v", err)
	}
	if svc.createInput.CreateBy != 0 || svc.createInput.UpdateBy != 0 {
		t.Fatalf("audit fields = %d/%d, want the system actor 0", svc.createInput.CreateBy, svc.createInput.UpdateBy)
	}
}

func TestCreateConfig_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeConfigsService{createErr: errServiceFailed}
	app, _ := newParamApp(svc)

	if _, err := app.CreateConfig(context.Background(), &paramv1.CreateConfigRequest{Config: &paramv1.Config{ConfigName: "n", ConfigKey: "sys.k"}}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestGetConfig_ConvertsDTO(t *testing.T) {
	now := time.Now()
	svc := &fakeConfigsService{get: &paramentity.Config{
		ID: 7, ConfigName: "注册开关", ConfigKey: "sys.account.registerUser", ConfigValue: "false",
		BuiltIn: true, Remark: "备注", CreateBy: 1, UpdateBy: 2, CreatedAt: now, UpdatedAt: now,
	}}
	app, _ := newParamApp(svc)

	got, err := app.GetConfig(context.Background(), &paramv1.GetConfigRequest{ConfigId: 7})
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if got.GetConfigId() != 7 || got.GetConfigName() != "注册开关" || got.GetConfigKey() != "sys.account.registerUser" ||
		got.GetConfigValue() != "false" || !got.GetBuiltIn() || got.GetRemark() != "备注" ||
		got.GetCreateBy() != 1 || got.GetUpdateBy() != 2 {
		t.Fatalf("reply = %#v", got)
	}
	if !got.GetCreatedAt().AsTime().Equal(now) || !got.GetUpdatedAt().AsTime().Equal(now) {
		t.Fatalf("timestamps = %v/%v", got.GetCreatedAt(), got.GetUpdatedAt())
	}
}

func TestListConfigs_IssuesNextPageToken(t *testing.T) {
	svc := &fakeConfigsService{list: []*paramentity.Config{{ID: 1}, {ID: 2}}}
	app, _ := newParamApp(svc)

	set, err := app.ListConfigs(context.Background(), &paramv1.ListConfigsRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("list configs: %v", err)
	}
	if len(set.GetConfigs()) != 2 || set.GetConfigs()[1].GetConfigId() != 2 {
		t.Fatalf("configs = %#v", set.GetConfigs())
	}
	if set.GetNextPageToken() == "" {
		t.Fatal("expected a next page token on a full page")
	}
	// Following the token resumes at the next offset.
	if _, err := app.ListConfigs(context.Background(), &paramv1.ListConfigsRequest{PageSize: 2, PageToken: set.GetNextPageToken()}); err != nil {
		t.Fatalf("list configs with token: %v", err)
	}
	if svc.listOffset != 2 {
		t.Fatalf("service saw offset %d after the token, want 2", svc.listOffset)
	}
}

func TestListConfigs_ParsesFilterOnDeclaredIdents(t *testing.T) {
	svc := &fakeConfigsService{}
	app, _ := newParamApp(svc)

	if _, err := app.ListConfigs(context.Background(), &paramv1.ListConfigsRequest{Filter: `config_key="sys.account.registerUser"`}); err != nil {
		t.Fatalf("filter on config_key: %v", err)
	}
	// AIP-160 tests booleans directly — a bare ident, never `= true`.
	if _, err := app.ListConfigs(context.Background(), &paramv1.ListConfigsRequest{Filter: `built_in`}); err != nil {
		t.Fatalf("filter on built_in: %v", err)
	}
	if _, err := app.ListConfigs(context.Background(), &paramv1.ListConfigsRequest{Filter: `config_name="x" AND NOT built_in`}); err != nil {
		t.Fatalf("combined filter: %v", err)
	}
	if _, err := app.ListConfigs(context.Background(), &paramv1.ListConfigsRequest{Filter: `nonsense_field="x"`}); err == nil {
		t.Fatal("filter on an undeclared ident must be rejected")
	}
}

func TestUpdateConfig_AppliesMaskAndCarriesKey(t *testing.T) {
	now := time.Now()
	svc := &fakeConfigsService{
		get:    &paramentity.Config{ID: 7, ConfigName: "注册开关", ConfigKey: "sys.account.registerUser", ConfigValue: "false", CreatedAt: now, UpdatedAt: now},
		update: &paramentity.Config{ID: 7, ConfigName: "注册开关", ConfigKey: "sys.account.registerUser", ConfigValue: "true", UpdateBy: 42},
	}
	app, tx := newParamApp(svc)

	got, err := app.UpdateConfig(actorContext(42), &paramv1.UpdateConfigRequest{
		Config:     &paramv1.Config{ConfigId: 7, ConfigValue: "true"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"config_value"}},
	})
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	// Unlike the dict module, the key travels with the update — masked-off
	// fields keep their stored value, so the entity carries the stored key.
	if svc.updateInput.ConfigKey != "sys.account.registerUser" {
		t.Fatalf("entity lost the stored key: %#v", svc.updateInput)
	}
	if svc.updateInput.ConfigValue != "true" {
		t.Fatalf("masked field not applied: %#v", svc.updateInput)
	}
	if svc.updateInput.UpdateBy != 42 {
		t.Fatalf("update audit = %d, want 42", svc.updateInput.UpdateBy)
	}
	if svc.updateCtx != tx.bodyCtx {
		t.Fatal("service Update must see the ctx the transaction body received")
	}
	if got.GetConfigValue() != "true" {
		t.Fatalf("reply = %#v", got)
	}
}

func TestUpdateConfig_IgnoresBuiltInMask(t *testing.T) {
	now := time.Now()
	// A stored built-in parameter; the client PATCHes built_in=false.
	svc := &fakeConfigsService{
		get:    &paramentity.Config{ID: 7, ConfigName: "注册开关", ConfigKey: "sys.account.registerUser", ConfigValue: "false", BuiltIn: true, CreatedAt: now, UpdatedAt: now},
		update: &paramentity.Config{ID: 7, ConfigName: "注册开关", ConfigKey: "sys.account.registerUser", ConfigValue: "false", BuiltIn: true},
	}
	app, _ := newParamApp(svc)

	_, err := app.UpdateConfig(context.Background(), &paramv1.UpdateConfigRequest{
		Config:     &paramv1.Config{ConfigId: 7, BuiltIn: false},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"config_value", "built_in"}},
	})
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	// The entity sent down never carries the client's flag — the stored one
	// rides along (and the domain service re-reads it regardless).
	if !svc.updateInput.BuiltIn {
		t.Fatal("client's built_in=false crossed into the update")
	}
}

func TestUpdateConfig_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeConfigsService{get: &paramentity.Config{ID: 7, ConfigName: "n", ConfigKey: "sys.k"}, updateErr: errServiceFailed}
	app, _ := newParamApp(svc)

	if _, err := app.UpdateConfig(context.Background(), &paramv1.UpdateConfigRequest{
		Config:     &paramv1.Config{ConfigId: 7},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"config_value"}},
	}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestDeleteConfig_StampsActorAndRunsInTx(t *testing.T) {
	svc := &fakeConfigsService{}
	app, tx := newParamApp(svc)

	if _, err := app.DeleteConfig(actorContext(42), &paramv1.DeleteConfigRequest{ConfigId: 7}); err != nil {
		t.Fatalf("delete config: %v", err)
	}
	if svc.deleteCalled != 1 || svc.deleteID != 7 || svc.deleteActor != 42 {
		t.Fatalf("delete call = %d/%d/%d, want 1/7/42", svc.deleteCalled, svc.deleteID, svc.deleteActor)
	}
	if svc.deleteCtx != tx.bodyCtx {
		t.Fatal("service Delete must see the ctx the transaction body received")
	}
}
