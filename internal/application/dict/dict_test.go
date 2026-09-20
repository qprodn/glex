package dict

import (
	"context"
	"errors"
	"testing"
	"time"

	dictv1 "glex/api/dict/v1"
	authnentity "glex/internal/domain/authn/entity"
	authnservice "glex/internal/domain/authn/service"
	dictentity "glex/internal/domain/dict/entity"
	dictservice "glex/internal/domain/dict/service"

	"glex/pkg/listq"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fakeDictService is a hand-rolled dictservice.Dict fake for the application
// tests: it records every call (including the ctx each write ran on) and
// returns the configured result, so the tests exercise orchestration only —
// transaction wrapping, DTO conversion, pagination, audit stamping — never
// the domain rules behind the interface.
type fakeDictService struct {
	// Configured returns.
	getType                                                                                 *dictentity.DictType
	listTypes                                                                               []*dictentity.DictType
	createType                                                                              *dictentity.DictType
	updateType                                                                              *dictentity.DictType
	getEntry                                                                                *dictentity.DictEntry
	listEntries                                                                             []*dictentity.DictEntry
	options                                                                                 []*dictentity.DictEntry
	createEntry                                                                             *dictentity.DictEntry
	updateEntry                                                                             *dictentity.DictEntry
	getTypeErr, listTypesErr, createTypeErr, updateTypeErr, deleteTypeErr                   error
	getEntryErr, listEntriesErr, optionsErr, createEntryErr, updateEntryErr, deleteEntryErr error

	// Recorded calls.
	createTypeCalled  int
	createTypeCtx     context.Context
	createTypeInput   *dictentity.DictType
	listTypesLimit    int
	listTypesOffset   int
	updateTypeCalled  int
	updateTypeCtx     context.Context
	updateTypeInput   *dictentity.DictType
	deleteTypeCalled  int
	deleteTypeCtx     context.Context
	deleteTypeID      int64
	deleteTypeActor   int64
	createEntryCalled int
	createEntryCtx    context.Context
	createEntryInput  *dictentity.DictEntry
	listEntriesLimit  int
	listEntriesOffset int
	updateEntryCalled int
	updateEntryCtx    context.Context
	updateEntryInput  *dictentity.DictEntry
	deleteEntryCalled int
	deleteEntryCtx    context.Context
	deleteEntryID     int64
	deleteEntryActor  int64
	optionsKey        string
}

func (f *fakeDictService) GetType(_ context.Context, _ int64) (*dictentity.DictType, error) {
	if f.getTypeErr != nil {
		return nil, f.getTypeErr
	}
	return f.getType, nil
}

func (f *fakeDictService) ListTypes(_ context.Context, q listq.Query) ([]*dictentity.DictType, error) {
	if f.listTypesErr != nil {
		return nil, f.listTypesErr
	}
	f.listTypesLimit, f.listTypesOffset = q.Limit, q.Offset
	return f.listTypes, nil
}

func (f *fakeDictService) CreateType(ctx context.Context, t *dictentity.DictType) (*dictentity.DictType, error) {
	f.createTypeCalled++
	f.createTypeCtx, f.createTypeInput = ctx, t
	if f.createTypeErr != nil {
		return nil, f.createTypeErr
	}
	return f.createType, nil
}

func (f *fakeDictService) UpdateType(ctx context.Context, t *dictentity.DictType) (*dictentity.DictType, error) {
	f.updateTypeCalled++
	f.updateTypeCtx, f.updateTypeInput = ctx, t
	if f.updateTypeErr != nil {
		return nil, f.updateTypeErr
	}
	return f.updateType, nil
}

func (f *fakeDictService) DeleteType(ctx context.Context, req *dictservice.DeleteRequest) error {
	f.deleteTypeCalled++
	f.deleteTypeCtx, f.deleteTypeID, f.deleteTypeActor = ctx, req.ID, req.ActorID
	return f.deleteTypeErr
}

func (f *fakeDictService) GetEntry(_ context.Context, _ int64) (*dictentity.DictEntry, error) {
	if f.getEntryErr != nil {
		return nil, f.getEntryErr
	}
	return f.getEntry, nil
}

func (f *fakeDictService) ListEntries(_ context.Context, q listq.Query) ([]*dictentity.DictEntry, error) {
	if f.listEntriesErr != nil {
		return nil, f.listEntriesErr
	}
	f.listEntriesLimit, f.listEntriesOffset = q.Limit, q.Offset
	return f.listEntries, nil
}

func (f *fakeDictService) OptionsByType(_ context.Context, key string) ([]*dictentity.DictEntry, error) {
	f.optionsKey = key
	if f.optionsErr != nil {
		return nil, f.optionsErr
	}
	return f.options, nil
}

func (f *fakeDictService) CreateEntry(ctx context.Context, e *dictentity.DictEntry) (*dictentity.DictEntry, error) {
	f.createEntryCalled++
	f.createEntryCtx, f.createEntryInput = ctx, e
	if f.createEntryErr != nil {
		return nil, f.createEntryErr
	}
	return f.createEntry, nil
}

func (f *fakeDictService) UpdateEntry(ctx context.Context, e *dictentity.DictEntry) (*dictentity.DictEntry, error) {
	f.updateEntryCalled++
	f.updateEntryCtx, f.updateEntryInput = ctx, e
	if f.updateEntryErr != nil {
		return nil, f.updateEntryErr
	}
	return f.updateEntry, nil
}

func (f *fakeDictService) DeleteEntry(ctx context.Context, req *dictservice.DeleteRequest) error {
	f.deleteEntryCalled++
	f.deleteEntryCtx, f.deleteEntryID, f.deleteEntryActor = ctx, req.ID, req.ActorID
	return f.deleteEntryErr
}

// errServiceFailed is the propagation sentinel: application tests only assert
// that a service error crosses the handler untouched.
var errServiceFailed = errors.New("service failed")

// actorContext returns a ctx authenticating the given System User, so tests
// can assert the audit stamping.
func actorContext(userID int64) context.Context {
	return authnservice.WithPrincipal(context.Background(), authnentity.NewPrincipal(userID, "alice", "session-id", nil, nil))
}

func newDictApp(svc *fakeDictService) (*DictApplication, *fakeTransaction) {
	tx := newFakeTransaction()
	return NewDictApplication(svc, tx, testLogger()), tx
}

// --- Dict Type handlers ---

func TestCreateDictType_StampsActorAndRunsInTx(t *testing.T) {
	svc := &fakeDictService{createType: &dictentity.DictType{ID: 7, DictName: "系统启用停用", DictType: "sys_normal_disable", Remark: "r"}}
	app, tx := newDictApp(svc)

	got, err := app.CreateDictType(actorContext(42), &dictv1.CreateDictTypeRequest{
		DictType: &dictv1.DictType{DictName: "系统启用停用", DictType: "sys_normal_disable", Remark: "r"},
	})
	if err != nil {
		t.Fatalf("create dict type: %v", err)
	}
	if svc.createTypeCalled != 1 {
		t.Fatalf("expected exactly one service CreateType, got %d", svc.createTypeCalled)
	}
	// The write runs on the ctx the transaction body received.
	if svc.createTypeCtx != tx.bodyCtx {
		t.Fatal("service CreateType must see the ctx the transaction body received")
	}
	// The acting Principal is stamped into the audit fields.
	if svc.createTypeInput.CreateBy != 42 || svc.createTypeInput.UpdateBy != 42 {
		t.Fatalf("audit fields = %d/%d, want the acting principal 42", svc.createTypeInput.CreateBy, svc.createTypeInput.UpdateBy)
	}
	if got.GetDictTypeId() != 7 || got.GetDictType() != "sys_normal_disable" || got.GetRemark() != "r" {
		t.Fatalf("reply = %#v", got)
	}
}

func TestCreateDictType_SystemActorWhenUnauthenticated(t *testing.T) {
	svc := &fakeDictService{createType: &dictentity.DictType{}}
	app, _ := newDictApp(svc)

	if _, err := app.CreateDictType(context.Background(), &dictv1.CreateDictTypeRequest{DictType: &dictv1.DictType{DictName: "n", DictType: "k"}}); err != nil {
		t.Fatalf("create dict type: %v", err)
	}
	if svc.createTypeInput.CreateBy != 0 || svc.createTypeInput.UpdateBy != 0 {
		t.Fatalf("audit fields = %d/%d, want the system actor 0", svc.createTypeInput.CreateBy, svc.createTypeInput.UpdateBy)
	}
}

func TestCreateDictType_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeDictService{createTypeErr: errServiceFailed}
	app, _ := newDictApp(svc)

	if _, err := app.CreateDictType(context.Background(), &dictv1.CreateDictTypeRequest{DictType: &dictv1.DictType{DictName: "n", DictType: "k"}}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

func TestGetDictType_ConvertsDTO(t *testing.T) {
	now := time.Now()
	svc := &fakeDictService{getType: &dictentity.DictType{
		ID: 7, DictName: "系统启用停用", DictType: "sys_normal_disable", Remark: "备注",
		CreateBy: 1, UpdateBy: 2, CreatedAt: now, UpdatedAt: now,
	}}
	app, _ := newDictApp(svc)

	got, err := app.GetDictType(context.Background(), &dictv1.GetDictTypeRequest{DictTypeId: 7})
	if err != nil {
		t.Fatalf("get dict type: %v", err)
	}
	if got.GetDictTypeId() != 7 || got.GetDictName() != "系统启用停用" || got.GetDictType() != "sys_normal_disable" ||
		got.GetRemark() != "备注" || got.GetCreateBy() != 1 || got.GetUpdateBy() != 2 {
		t.Fatalf("reply = %#v", got)
	}
	if !got.GetCreatedAt().AsTime().Equal(now) || !got.GetUpdatedAt().AsTime().Equal(now) {
		t.Fatalf("timestamps = %v/%v", got.GetCreatedAt(), got.GetUpdatedAt())
	}
}

func TestListDictTypes_IssuesNextPageToken(t *testing.T) {
	svc := &fakeDictService{listTypes: []*dictentity.DictType{{ID: 1}, {ID: 2}}}
	app, _ := newDictApp(svc)

	set, err := app.ListDictTypes(context.Background(), &dictv1.ListDictTypesRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("list dict types: %v", err)
	}
	if len(set.GetDictTypes()) != 2 || set.GetDictTypes()[1].GetDictTypeId() != 2 {
		t.Fatalf("dict types = %#v", set.GetDictTypes())
	}
	if set.GetNextPageToken() == "" {
		t.Fatal("expected a next page token on a full page")
	}
	// Following the token resumes at the next offset.
	if _, err := app.ListDictTypes(context.Background(), &dictv1.ListDictTypesRequest{PageSize: 2, PageToken: set.GetNextPageToken()}); err != nil {
		t.Fatalf("list dict types with token: %v", err)
	}
	if svc.listTypesOffset != 2 {
		t.Fatalf("service saw offset %d after the token, want 2", svc.listTypesOffset)
	}
}

func TestUpdateDictType_NeverCarriesClientKey(t *testing.T) {
	current := &dictentity.DictType{ID: 5, DictName: "系统启用停用", DictType: "sys_normal_disable"}
	svc := &fakeDictService{getType: current, updateType: &dictentity.DictType{ID: 5, DictName: "启用停用", DictType: "sys_normal_disable"}}
	app, _ := newDictApp(svc)

	got, err := app.UpdateDictType(actorContext(42), &dictv1.UpdateDictTypeRequest{
		DictType:   &dictv1.DictType{DictTypeId: 5, DictName: "启用停用", DictType: "hacked_key"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"dict_name"}},
	})
	if err != nil {
		t.Fatalf("update dict type: %v", err)
	}
	if got.GetDictName() != "启用停用" {
		t.Fatalf("reply = %#v", got)
	}
	// The entity sent to the service carries no dict_type at all: the
	// immutable key (ADR 0006) is re-read from storage there.
	if svc.updateTypeInput.DictType != "" {
		t.Fatalf("entity carried dict_type %q; the application must never send it", svc.updateTypeInput.DictType)
	}
	if svc.updateTypeInput.DictName != "启用停用" || svc.updateTypeInput.UpdateBy != 42 {
		t.Fatalf("entity = %#v", svc.updateTypeInput)
	}
}

func TestDeleteDictType_PassesIDAndActor(t *testing.T) {
	svc := &fakeDictService{}
	app, tx := newDictApp(svc)

	if _, err := app.DeleteDictType(actorContext(42), &dictv1.DeleteDictTypeRequest{DictTypeId: 3}); err != nil {
		t.Fatalf("delete dict type: %v", err)
	}
	if svc.deleteTypeID != 3 || svc.deleteTypeActor != 42 {
		t.Fatalf("service saw id/actor %d/%d, want 3/42", svc.deleteTypeID, svc.deleteTypeActor)
	}
	if svc.deleteTypeCtx != tx.bodyCtx {
		t.Fatal("service DeleteType must see the ctx the transaction body received")
	}
}

func TestDeleteDictType_ServiceErrorPropagates(t *testing.T) {
	svc := &fakeDictService{deleteTypeErr: errServiceFailed}
	app, _ := newDictApp(svc)

	if _, err := app.DeleteDictType(context.Background(), &dictv1.DeleteDictTypeRequest{DictTypeId: 3}); !errors.Is(err, errServiceFailed) {
		t.Fatalf("expected the service error to propagate, got %v", err)
	}
}

// --- Dict Entry handlers ---

func TestCreateDictEntry_MapsTagStyleAndStampsActor(t *testing.T) {
	svc := &fakeDictService{createEntry: &dictentity.DictEntry{ID: 9, DictType: "sys_normal_disable", DictLabel: "正常", DictValue: "0", TagStyle: "primary"}}
	app, _ := newDictApp(svc)

	got, err := app.CreateDictEntry(actorContext(42), &dictv1.CreateDictEntryRequest{
		Entry: &dictv1.DictEntry{
			DictType: "sys_normal_disable", DictLabel: "正常", DictValue: "0",
			TagStyle: dictv1.TagStyle_TAG_STYLE_PRIMARY,
		},
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	// The wire enum maps to the stored preset (RuoYi list_class) and back.
	if svc.createEntryInput.TagStyle != "primary" {
		t.Fatalf("stored tag style = %q, want primary", svc.createEntryInput.TagStyle)
	}
	if got.GetTagStyle() != dictv1.TagStyle_TAG_STYLE_PRIMARY {
		t.Fatalf("reply tag style = %v", got.GetTagStyle())
	}
	if svc.createEntryInput.CreateBy != 42 {
		t.Fatalf("audit actor = %d, want 42", svc.createEntryInput.CreateBy)
	}
}

func TestCreateDictEntry_UnspecifiedTagStyleNormalizes(t *testing.T) {
	svc := &fakeDictService{createEntry: &dictentity.DictEntry{}}
	app, _ := newDictApp(svc)

	if _, err := app.CreateDictEntry(context.Background(), &dictv1.CreateDictEntryRequest{
		Entry: &dictv1.DictEntry{DictType: "k", DictLabel: "l", DictValue: "v"},
	}); err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if svc.createEntryInput.TagStyle != "default" {
		t.Fatalf("unspecified tag style must normalize to default, got %q", svc.createEntryInput.TagStyle)
	}
}

func TestUpdateDictEntry_NeverCarriesClientType(t *testing.T) {
	current := &dictentity.DictEntry{ID: 9, DictType: "sys_normal_disable", DictLabel: "正常", DictValue: "0", TagStyle: "primary"}
	svc := &fakeDictService{getEntry: current, updateEntry: current}
	app, _ := newDictApp(svc)

	if _, err := app.UpdateDictEntry(actorContext(42), &dictv1.UpdateDictEntryRequest{
		Entry:      &dictv1.DictEntry{EntryId: 9, DictType: "hacked_type", DictLabel: "正常"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"dict_label"}},
	}); err != nil {
		t.Fatalf("update entry: %v", err)
	}
	if svc.updateEntryInput.DictType != "" {
		t.Fatalf("entity carried dict_type %q; the application must never send it", svc.updateEntryInput.DictType)
	}
	if svc.updateEntryInput.DictLabel != "正常" || svc.updateEntryInput.TagStyle != "primary" || svc.updateEntryInput.UpdateBy != 42 {
		t.Fatalf("entity = %#v", svc.updateEntryInput)
	}
}

func TestListDictEntries_IssuesNextPageToken(t *testing.T) {
	svc := &fakeDictService{listEntries: []*dictentity.DictEntry{{ID: 1}, {ID: 2}}}
	app, _ := newDictApp(svc)

	set, err := app.ListDictEntries(context.Background(), &dictv1.ListDictEntriesRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(set.GetEntries()) != 2 || set.GetNextPageToken() == "" {
		t.Fatalf("entries = %#v, token = %q", set.GetEntries(), set.GetNextPageToken())
	}
}

func TestListDictEntriesByType_ConvertsLightweightOptions(t *testing.T) {
	svc := &fakeDictService{options: []*dictentity.DictEntry{
		{DictValue: "0", DictLabel: "正常", TagStyle: "primary", IsDefault: true},
		{DictValue: "1", DictLabel: "停用", TagStyle: "danger"},
	}}
	app, _ := newDictApp(svc)

	set, err := app.ListDictEntriesByType(context.Background(), &dictv1.ListDictEntriesByTypeRequest{DictType: "sys_normal_disable"})
	if err != nil {
		t.Fatalf("options by type: %v", err)
	}
	if svc.optionsKey != "sys_normal_disable" {
		t.Fatalf("service saw key %q", svc.optionsKey)
	}
	options := set.GetOptions()
	if len(options) != 2 {
		t.Fatalf("options = %#v", options)
	}
	if options[0].GetValue() != "0" || options[0].GetLabel() != "正常" ||
		options[0].GetTagStyle() != dictv1.TagStyle_TAG_STYLE_PRIMARY || !options[0].GetIsDefault() {
		t.Fatalf("first option = %#v", options[0])
	}
	if options[1].GetTagStyle() != dictv1.TagStyle_TAG_STYLE_DANGER || options[1].GetIsDefault() {
		t.Fatalf("second option = %#v", options[1])
	}
	// The lightweight view carries no audit fields to verify — the type
	// system enforces it (DictEntryOption has none).
}

func TestListDictEntriesByType_UnknownKeyIsEmptyList(t *testing.T) {
	svc := &fakeDictService{}
	app, _ := newDictApp(svc)

	set, err := app.ListDictEntriesByType(context.Background(), &dictv1.ListDictEntriesByTypeRequest{DictType: "sys_nope"})
	if err != nil {
		t.Fatalf("unknown key must yield an empty list, got error %v", err)
	}
	if len(set.GetOptions()) != 0 {
		t.Fatalf("expected empty options, got %#v", set.GetOptions())
	}
}
