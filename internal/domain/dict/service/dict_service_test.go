package service

import (
	"context"
	"testing"

	"glex/internal/domain/dict"
	"glex/internal/domain/dict/entity"
	"glex/pkg/listq"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeTypeRepo is an in-memory repository.DictTypeRepo for testing the dict
// domain service in isolation from the infrastructure layer.
type fakeTypeRepo struct {
	byID    map[int64]*entity.DictType
	byKey   map[string]*entity.DictType
	nextID  int64
	created []*entity.DictType
	updated []*entity.DictType
	deleted []int64
}

func newFakeTypeRepo() *fakeTypeRepo {
	return &fakeTypeRepo{byID: make(map[int64]*entity.DictType), byKey: make(map[string]*entity.DictType), nextID: 1}
}

func (r *fakeTypeRepo) FindByID(_ context.Context, id int64) (*entity.DictType, error) {
	t, ok := r.byID[id]
	if !ok {
		return nil, dict.ErrDictTypeNotFound
	}
	return t, nil
}

func (r *fakeTypeRepo) FindByKey(_ context.Context, key string) (*entity.DictType, error) {
	t, ok := r.byKey[key]
	if !ok {
		return nil, dict.ErrDictTypeNotFound
	}
	return t, nil
}

func (r *fakeTypeRepo) List(context.Context, listq.Query) ([]*entity.DictType, error) {
	return nil, nil
}

func (r *fakeTypeRepo) Create(_ context.Context, t *entity.DictType) (*entity.DictType, error) {
	t.ID = r.nextID
	r.nextID++
	r.byID[t.ID] = t
	r.byKey[t.DictType] = t
	r.created = append(r.created, t)
	return t, nil
}

func (r *fakeTypeRepo) Update(_ context.Context, t *entity.DictType) (*entity.DictType, error) {
	if _, ok := r.byID[t.ID]; !ok {
		return nil, dict.ErrDictTypeNotFound
	}
	r.byID[t.ID] = t
	r.updated = append(r.updated, t)
	return t, nil
}

func (r *fakeTypeRepo) Delete(_ context.Context, id, _ int64) error {
	if _, ok := r.byID[id]; !ok {
		return dict.ErrDictTypeNotFound
	}
	r.deleted = append(r.deleted, id)
	return nil
}

// fakeEntryRepo is an in-memory repository.DictEntryRepo.
type fakeEntryRepo struct {
	byID    map[int64]*entity.DictEntry
	byPair  map[string]*entity.DictEntry // "type\x00value" → entry
	nextID  int64
	count   map[string]int // key → active entry count
	created []*entity.DictEntry
	updated []*entity.DictEntry
	deleted []int64
	options []*entity.DictEntry
}

func newFakeEntryRepo() *fakeEntryRepo {
	return &fakeEntryRepo{byID: make(map[int64]*entity.DictEntry), byPair: make(map[string]*entity.DictEntry), count: make(map[string]int), nextID: 1}
}

func (r *fakeEntryRepo) FindByID(_ context.Context, id int64) (*entity.DictEntry, error) {
	e, ok := r.byID[id]
	if !ok {
		return nil, dict.ErrDictEntryNotFound
	}
	return e, nil
}

func (r *fakeEntryRepo) FindByTypeAndValue(_ context.Context, dictType, value string) (*entity.DictEntry, error) {
	e, ok := r.byPair[dictType+"\x00"+value]
	if !ok {
		return nil, dict.ErrDictEntryNotFound
	}
	return e, nil
}

func (r *fakeEntryRepo) List(context.Context, listq.Query) ([]*entity.DictEntry, error) {
	return nil, nil
}

func (r *fakeEntryRepo) ListByDictKey(_ context.Context, key string) ([]*entity.DictEntry, error) {
	if r.options != nil {
		return r.options, nil
	}
	return nil, nil
}

func (r *fakeEntryRepo) CountByDictKey(_ context.Context, key string) (int, error) {
	return r.count[key], nil
}

func (r *fakeEntryRepo) Create(_ context.Context, e *entity.DictEntry) (*entity.DictEntry, error) {
	e.ID = r.nextID
	r.nextID++
	r.byID[e.ID] = e
	r.byPair[e.DictType+"\x00"+e.DictValue] = e
	r.count[e.DictType]++
	r.created = append(r.created, e)
	return e, nil
}

func (r *fakeEntryRepo) Update(_ context.Context, e *entity.DictEntry) (*entity.DictEntry, error) {
	if _, ok := r.byID[e.ID]; !ok {
		return nil, dict.ErrDictEntryNotFound
	}
	r.byID[e.ID] = e
	r.byPair[e.DictType+"\x00"+e.DictValue] = e
	r.updated = append(r.updated, e)
	return e, nil
}

func (r *fakeEntryRepo) Delete(_ context.Context, id, _ int64) error {
	if _, ok := r.byID[id]; !ok {
		return dict.ErrDictEntryNotFound
	}
	r.deleted = append(r.deleted, id)
	return nil
}

func newDictSvc(types *fakeTypeRepo, entries *fakeEntryRepo) Dict {
	return NewDictService(types, entries, testLogger())
}

// --- Dict Type (字典类型) ---

func TestCreateType_PersistsValidatedType(t *testing.T) {
	repo := newFakeTypeRepo()
	svc := newDictSvc(repo, newFakeEntryRepo())

	created, err := svc.CreateType(context.Background(), &entity.DictType{DictName: "系统启用停用", DictType: "sys_normal_disable"})
	if err != nil {
		t.Fatalf("create type: %v", err)
	}
	if created.ID == 0 || len(repo.created) != 1 {
		t.Fatalf("expected one persisted type, got %#v", repo.created)
	}
}

func TestCreateType_BlankNameRejected(t *testing.T) {
	svc := newDictSvc(newFakeTypeRepo(), newFakeEntryRepo())

	_, err := svc.CreateType(context.Background(), &entity.DictType{DictName: "", DictType: "sys_normal_disable"})
	if !errors.Is(err, dict.ErrDictTypeInvalid) {
		t.Fatalf("expected ErrDictTypeInvalid, got %v", err)
	}
}

func TestCreateType_BadKeyRejected(t *testing.T) {
	svc := newDictSvc(newFakeTypeRepo(), newFakeEntryRepo())

	for _, key := range []string{"", "Sys_Normal", "1abc", "sys-normal", "正常"} {
		if _, err := svc.CreateType(context.Background(), &entity.DictType{DictName: "系统启用停用", DictType: key}); !errors.Is(err, dict.ErrDictTypeInvalid) {
			t.Fatalf("key %q: expected ErrDictTypeInvalid, got %v", key, err)
		}
	}
	// The full legal charset passes.
	if _, err := svc.CreateType(context.Background(), &entity.DictType{DictName: "x", DictType: "a0_z"}); err != nil {
		t.Fatalf("legal key rejected: %v", err)
	}
}

func TestCreateType_KeyConflict(t *testing.T) {
	repo := newFakeTypeRepo()
	repo.byKey["sys_normal_disable"] = &entity.DictType{ID: 7, DictName: "已有", DictType: "sys_normal_disable"}
	svc := newDictSvc(repo, newFakeEntryRepo())

	_, err := svc.CreateType(context.Background(), &entity.DictType{DictName: "系统启用停用", DictType: "sys_normal_disable"})
	if !errors.Is(err, dict.ErrDictTypeConflict) {
		t.Fatalf("expected ErrDictTypeConflict, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatal("expected no type to be created on conflict")
	}
}

func TestUpdateType_IgnoresIncomingKey(t *testing.T) {
	repo := newFakeTypeRepo()
	repo.byID[5] = &entity.DictType{ID: 5, DictName: "系统启用停用", DictType: "sys_normal_disable"}
	svc := newDictSvc(repo, newFakeEntryRepo())

	// A client somehow sends a different key (mask or not): the stored key
	// wins (ADR 0006).
	updated, err := svc.UpdateType(context.Background(), &entity.DictType{ID: 5, DictName: "启用停用", DictType: "hacked_key"})
	if err != nil {
		t.Fatalf("update type: %v", err)
	}
	if updated.DictType != "sys_normal_disable" {
		t.Fatalf("stored key must win, got %q", updated.DictType)
	}
}

func TestUpdateType_NotFound(t *testing.T) {
	svc := newDictSvc(newFakeTypeRepo(), newFakeEntryRepo())

	if _, err := svc.UpdateType(context.Background(), &entity.DictType{ID: 99, DictName: "x", DictType: "k"}); !errors.Is(err, dict.ErrDictTypeNotFound) {
		t.Fatalf("expected ErrDictTypeNotFound, got %v", err)
	}
}

func TestDeleteType_InUseRejected(t *testing.T) {
	repo := newFakeTypeRepo()
	repo.byID[5] = &entity.DictType{ID: 5, DictName: "系统启用停用", DictType: "sys_normal_disable"}
	entries := newFakeEntryRepo()
	entries.count["sys_normal_disable"] = 2
	svc := newDictSvc(repo, entries)

	if err := svc.DeleteType(context.Background(), 5, 0); !errors.Is(err, dict.ErrDictTypeInUse) {
		t.Fatalf("expected ErrDictTypeInUse, got %v", err)
	}
	if len(repo.deleted) != 0 {
		t.Fatal("type must not be deleted while entries exist")
	}
}

func TestDeleteType_EmptyAllowed(t *testing.T) {
	repo := newFakeTypeRepo()
	repo.byID[5] = &entity.DictType{ID: 5, DictName: "系统启用停用", DictType: "sys_normal_disable"}
	svc := newDictSvc(repo, newFakeEntryRepo())

	if err := svc.DeleteType(context.Background(), 5, 42); err != nil {
		t.Fatalf("delete empty type: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 5 {
		t.Fatalf("expected type 5 deleted, got %v", repo.deleted)
	}
}

func TestDeleteType_NotFound(t *testing.T) {
	svc := newDictSvc(newFakeTypeRepo(), newFakeEntryRepo())

	if err := svc.DeleteType(context.Background(), 99, 0); !errors.Is(err, dict.ErrDictTypeNotFound) {
		t.Fatalf("expected ErrDictTypeNotFound, got %v", err)
	}
}

// --- Dict Entry (字典项) ---

func TestCreateEntry_PersistsValidatedEntry(t *testing.T) {
	types := newFakeTypeRepo()
	types.byKey["sys_normal_disable"] = &entity.DictType{ID: 5, DictType: "sys_normal_disable"}
	entries := newFakeEntryRepo()
	svc := newDictSvc(types, entries)

	created, err := svc.CreateEntry(context.Background(), &entity.DictEntry{
		DictType: "sys_normal_disable", DictLabel: "正常", DictValue: "0", TagStyle: "primary",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if created.ID == 0 || len(entries.created) != 1 {
		t.Fatalf("expected one persisted entry, got %#v", entries.created)
	}
}

func TestCreateEntry_BlankLabelOrValueRejected(t *testing.T) {
	types := newFakeTypeRepo()
	types.byKey["sys_normal_disable"] = &entity.DictType{ID: 5, DictType: "sys_normal_disable"}
	svc := newDictSvc(types, newFakeEntryRepo())

	for _, e := range []*entity.DictEntry{
		{DictType: "sys_normal_disable", DictLabel: "", DictValue: "0", TagStyle: "default"},
		{DictType: "sys_normal_disable", DictLabel: "正常", DictValue: "", TagStyle: "default"},
	} {
		if _, err := svc.CreateEntry(context.Background(), e); !errors.Is(err, dict.ErrDictEntryInvalid) {
			t.Fatalf("expected ErrDictEntryInvalid, got %v", err)
		}
	}
}

func TestCreateEntry_BadTagStyleRejected(t *testing.T) {
	types := newFakeTypeRepo()
	types.byKey["sys_normal_disable"] = &entity.DictType{ID: 5, DictType: "sys_normal_disable"}
	svc := newDictSvc(types, newFakeEntryRepo())

	_, err := svc.CreateEntry(context.Background(), &entity.DictEntry{
		DictType: "sys_normal_disable", DictLabel: "正常", DictValue: "0", TagStyle: "neon",
	})
	if !errors.Is(err, dict.ErrDictEntryInvalid) {
		t.Fatalf("expected ErrDictEntryInvalid, got %v", err)
	}
}

func TestCreateEntry_UnknownTypeRejected(t *testing.T) {
	entries := newFakeEntryRepo()
	svc := newDictSvc(newFakeTypeRepo(), entries)

	_, err := svc.CreateEntry(context.Background(), &entity.DictEntry{
		DictType: "sys_nope", DictLabel: "正常", DictValue: "0", TagStyle: "default",
	})
	if !errors.Is(err, dict.ErrDictTypeNotFound) {
		t.Fatalf("expected ErrDictTypeNotFound, got %v", err)
	}
	if len(entries.created) != 0 {
		t.Fatal("expected no entry to be created under an unknown type")
	}
}

func TestCreateEntry_ValueConflict(t *testing.T) {
	types := newFakeTypeRepo()
	types.byKey["sys_normal_disable"] = &entity.DictType{ID: 5, DictType: "sys_normal_disable"}
	entries := newFakeEntryRepo()
	entries.byPair["sys_normal_disable\x000"] = &entity.DictEntry{ID: 9, DictType: "sys_normal_disable", DictValue: "0"}
	svc := newDictSvc(types, entries)

	_, err := svc.CreateEntry(context.Background(), &entity.DictEntry{
		DictType: "sys_normal_disable", DictLabel: "正常(重复值)", DictValue: "0", TagStyle: "default",
	})
	if !errors.Is(err, dict.ErrDictEntryConflict) {
		t.Fatalf("expected ErrDictEntryConflict, got %v", err)
	}
}

func TestUpdateEntry_IgnoresIncomingType(t *testing.T) {
	entries := newFakeEntryRepo()
	entries.byID[9] = &entity.DictEntry{ID: 9, DictType: "sys_normal_disable", DictLabel: "正常", DictValue: "0"}
	svc := newDictSvc(newFakeTypeRepo(), entries)

	updated, err := svc.UpdateEntry(context.Background(), &entity.DictEntry{
		ID: 9, DictType: "sys_user_gender", DictLabel: "正常", DictValue: "0", TagStyle: "default",
	})
	if err != nil {
		t.Fatalf("update entry: %v", err)
	}
	if updated.DictType != "sys_normal_disable" {
		t.Fatalf("stored owning type must win (ADR 0006), got %q", updated.DictType)
	}
}

func TestUpdateEntry_ConflictExcludingSelf(t *testing.T) {
	entries := newFakeEntryRepo()
	entries.byID[9] = &entity.DictEntry{ID: 9, DictType: "sys_normal_disable", DictValue: "0"}
	entries.byID[10] = &entity.DictEntry{ID: 10, DictType: "sys_normal_disable", DictValue: "1"}
	entries.byPair["sys_normal_disable\x000"] = entries.byID[9]
	entries.byPair["sys_normal_disable\x001"] = entries.byID[10]
	svc := newDictSvc(newFakeTypeRepo(), entries)

	// Taking another entry's value is rejected...
	_, err := svc.UpdateEntry(context.Background(), &entity.DictEntry{ID: 9, DictType: "sys_normal_disable", DictLabel: "x", DictValue: "1", TagStyle: "default"})
	if !errors.Is(err, dict.ErrDictEntryConflict) {
		t.Fatalf("expected ErrDictEntryConflict, got %v", err)
	}
	// ...but keeping its own value passes.
	if _, err := svc.UpdateEntry(context.Background(), &entity.DictEntry{ID: 9, DictType: "sys_normal_disable", DictLabel: "正常", DictValue: "0", TagStyle: "default"}); err != nil {
		t.Fatalf("update keeping own value: %v", err)
	}
}

func TestUpdateEntry_NotFound(t *testing.T) {
	svc := newDictSvc(newFakeTypeRepo(), newFakeEntryRepo())

	if _, err := svc.UpdateEntry(context.Background(), &entity.DictEntry{ID: 99, DictType: "k", DictLabel: "x", DictValue: "0", TagStyle: "default"}); !errors.Is(err, dict.ErrDictEntryNotFound) {
		t.Fatalf("expected ErrDictEntryNotFound, got %v", err)
	}
}

func TestDeleteEntry_NotFound(t *testing.T) {
	svc := newDictSvc(newFakeTypeRepo(), newFakeEntryRepo())

	if err := svc.DeleteEntry(context.Background(), 99, 0); !errors.Is(err, dict.ErrDictEntryNotFound) {
		t.Fatalf("expected ErrDictEntryNotFound, got %v", err)
	}
}

func TestOptionsByType_UnknownKeyIsEmptyList(t *testing.T) {
	svc := newDictSvc(newFakeTypeRepo(), newFakeEntryRepo())

	options, err := svc.OptionsByType(context.Background(), "sys_nope")
	if err != nil {
		t.Fatalf("unknown key must yield an empty list, got error %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("expected empty options, got %#v", options)
	}
}
