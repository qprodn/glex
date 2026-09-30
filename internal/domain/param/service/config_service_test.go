package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"glex/internal/domain/param"
	"glex/internal/domain/param/entity"
	"glex/internal/domain/param/repository"
	"glex/pkg/listq"
)

// fakeConfigRepo is an in-memory repository.ConfigRepo for testing the param
// domain service in isolation from the infrastructure layer.
type fakeConfigRepo struct {
	byID    map[int64]*entity.Config
	byKey   map[string]*entity.Config
	nextID  int64
	deleted []repository.DeleteRequest
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{byID: make(map[int64]*entity.Config), byKey: make(map[string]*entity.Config), nextID: 1}
}

func (r *fakeConfigRepo) FindByID(_ context.Context, id int64) (*entity.Config, error) {
	c, ok := r.byID[id]
	if !ok {
		return nil, param.ErrConfigNotFound
	}
	return c, nil
}

func (r *fakeConfigRepo) FindByKey(_ context.Context, key string) (*entity.Config, error) {
	c, ok := r.byKey[key]
	if !ok {
		return nil, param.ErrConfigNotFound
	}
	return c, nil
}

func (r *fakeConfigRepo) List(context.Context, listq.Query) ([]*entity.Config, error) {
	return nil, nil
}

func (r *fakeConfigRepo) Create(_ context.Context, c *entity.Config) (*entity.Config, error) {
	c.ID = r.nextID
	r.nextID++
	r.byID[c.ID] = c
	r.byKey[c.ConfigKey] = c
	return c, nil
}

func (r *fakeConfigRepo) Update(_ context.Context, c *entity.Config) (*entity.Config, error) {
	stored, ok := r.byID[c.ID]
	if !ok {
		return nil, param.ErrConfigNotFound
	}
	delete(r.byKey, stored.ConfigKey)
	r.byID[c.ID] = c
	r.byKey[c.ConfigKey] = c
	return c, nil
}

func (r *fakeConfigRepo) Delete(_ context.Context, req *repository.DeleteRequest) error {
	stored, ok := r.byID[req.ID]
	if !ok {
		return param.ErrConfigNotFound
	}
	delete(r.byID, req.ID)
	delete(r.byKey, stored.ConfigKey)
	r.deleted = append(r.deleted, *req)
	return nil
}

// seedBuiltIn installs one built-in and one ordinary parameter, as the seed
// data would.
func seedBuiltIn(repo *fakeConfigRepo) (*entity.Config, *entity.Config) {
	builtIn := &entity.Config{ConfigName: "注册开关", ConfigKey: "sys.account.registerUser", ConfigValue: "false", BuiltIn: true}
	ordinary := &entity.Config{ConfigName: "分页大小", ConfigKey: "ui.page.size", ConfigValue: "20"}
	repo.Create(context.Background(), builtIn)
	repo.Create(context.Background(), ordinary)
	return builtIn, ordinary
}

func newService(repo *fakeConfigRepo) Configs {
	return NewConfigsService(repo, testLogger())
}

func TestCreateValidatesBeforePersisting(t *testing.T) {
	repo := newFakeConfigRepo()
	svc := newService(repo)

	for name, c := range map[string]*entity.Config{
		"blank name":   {ConfigName: "", ConfigKey: "sys.ok"},
		"space name":   {ConfigName: "  \t ", ConfigKey: "sys.ok"}, // whitespace-only counts as blank (RuoYi @NotBlank)
		"blank key":    {ConfigName: "名字", ConfigKey: ""},
		"key charset":  {ConfigName: "名字", ConfigKey: "sys account"}, // space
		"key leading":  {ConfigName: "名字", ConfigKey: ".leading"},    // no leading letter/digit
		"value length": {ConfigName: "名字", ConfigKey: "sys.ok", ConfigValue: string(make([]byte, param.ValueMaxLen+1))},
		"name length":  {ConfigName: string(make([]byte, param.NameMaxLen+1)), ConfigKey: "sys.ok"},
		"remark runes": {ConfigName: "名字", ConfigKey: "sys.ok", Remark: strings.Repeat("备", param.RemarkMaxLen+1)},
	} {
		if _, err := svc.Create(context.Background(), c); !errors.Is(err, param.ErrConfigInvalid) {
			t.Errorf("%s: got %v, want ErrConfigInvalid", name, err)
		}
	}
	if len(repo.byID) != 0 {
		t.Fatalf("invalid create persisted %d rows", len(repo.byID))
	}
}

func TestCreateRejectsTakenKey(t *testing.T) {
	repo := newFakeConfigRepo()
	_, ordinary := seedBuiltIn(repo)
	svc := newService(repo)

	dup := &entity.Config{ConfigName: "另一个", ConfigKey: ordinary.ConfigKey, ConfigValue: "1"}
	if _, err := svc.Create(context.Background(), dup); !errors.Is(err, param.ErrConfigKeyConflict) {
		t.Fatalf("got %v, want ErrConfigKeyConflict", err)
	}
}

func TestCreatePersistsValidParameter(t *testing.T) {
	repo := newFakeConfigRepo()
	svc := newService(repo)

	c := &entity.Config{ConfigName: "分页大小", ConfigKey: "ui.page.size", ConfigValue: "20", CreateBy: 7, UpdateBy: 7}
	created, err := svc.Create(context.Background(), c)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.CreateBy != 7 {
		t.Fatalf("created row lost its id/audit stamps: %+v", created)
	}
	if _, err := svc.FindByKey(context.Background(), "ui.page.size"); err != nil {
		t.Fatalf("find_by_key after create: %v", err)
	}
}

func TestUpdateAllowsKeyRenameOntoFreeKey(t *testing.T) {
	repo := newFakeConfigRepo()
	_, ordinary := seedBuiltIn(repo)
	svc := newService(repo)

	// A fresh entity, as the application layer builds from the masked DTO —
	// never a mutation of the stored record.
	renamed := &entity.Config{
		ID: ordinary.ID, ConfigName: ordinary.ConfigName,
		ConfigKey: "ui.page.default-size", ConfigValue: ordinary.ConfigValue,
		UpdateBy: 5,
	}
	updated, err := svc.Update(context.Background(), renamed)
	if err != nil {
		t.Fatalf("rename onto free key: %v", err)
	}
	if updated.ConfigKey != "ui.page.default-size" {
		t.Fatalf("rename not persisted: %+v", updated)
	}
	if _, err := repo.FindByKey(context.Background(), "ui.page.size"); !errors.Is(err, param.ErrConfigNotFound) {
		t.Fatalf("old key still resolves after rename: %v", err)
	}
}

func TestUpdateRejectsRenameOntoTakenKey(t *testing.T) {
	repo := newFakeConfigRepo()
	builtIn, ordinary := seedBuiltIn(repo)
	svc := newService(repo)

	renamed := &entity.Config{ID: ordinary.ID, ConfigName: ordinary.ConfigName, ConfigKey: builtIn.ConfigKey}
	if _, err := svc.Update(context.Background(), renamed); !errors.Is(err, param.ErrConfigKeyConflict) {
		t.Fatalf("got %v, want ErrConfigKeyConflict", err)
	}
}

func TestUpdateKeepsOwnKey(t *testing.T) {
	repo := newFakeConfigRepo()
	_, ordinary := seedBuiltIn(repo)
	svc := newService(repo)

	resaved := &entity.Config{ID: ordinary.ID, ConfigName: ordinary.ConfigName, ConfigKey: ordinary.ConfigKey, ConfigValue: "50"}
	if _, err := svc.Update(context.Background(), resaved); err != nil {
		t.Fatalf("re-saving with the same key: %v", err)
	}
}

func TestUpdateValidatesUnknownRecord(t *testing.T) {
	repo := newFakeConfigRepo()
	svc := newService(repo)

	if _, err := svc.Update(context.Background(), &entity.Config{ID: 99, ConfigName: "x", ConfigKey: "sys.x"}); !errors.Is(err, param.ErrConfigNotFound) {
		t.Fatalf("got %v, want ErrConfigNotFound", err)
	}
}

func TestUpdateKeepsStoredBuiltInFlag(t *testing.T) {
	repo := newFakeConfigRepo()
	builtIn, _ := seedBuiltIn(repo)
	svc := newService(repo)

	// A PATCH carrying built_in=false must not strip the withdrawal
	// protection — the stored flag wins, whatever the caller sent.
	flipped := &entity.Config{
		ID: builtIn.ID, ConfigName: builtIn.ConfigName,
		ConfigKey: builtIn.ConfigKey, ConfigValue: builtIn.ConfigValue,
		BuiltIn: false,
	}
	updated, err := svc.Update(context.Background(), flipped)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !updated.BuiltIn {
		t.Fatal("built-in flag was stripped by the update")
	}
}

func TestDeleteRefusesBuiltIn(t *testing.T) {
	repo := newFakeConfigRepo()
	builtIn, _ := seedBuiltIn(repo)
	svc := newService(repo)

	if err := svc.Delete(context.Background(), &DeleteRequest{ID: builtIn.ID, ActorID: 5}); !errors.Is(err, param.ErrConfigBuiltInLocked) {
		t.Fatalf("got %v, want ErrConfigBuiltInLocked", err)
	}
	if len(repo.deleted) != 0 {
		t.Fatalf("built-in delete reached the repo: %+v", repo.deleted)
	}
}

func TestDeleteWithdrawsOrdinaryParameterWithActor(t *testing.T) {
	repo := newFakeConfigRepo()
	_, ordinary := seedBuiltIn(repo)
	svc := newService(repo)

	if err := svc.Delete(context.Background(), &DeleteRequest{ID: ordinary.ID, ActorID: 5}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0].ID != ordinary.ID || repo.deleted[0].ActorID != 5 {
		t.Fatalf("delete did not stamp id+actor: %+v", repo.deleted)
	}
}

func TestFindByKeyMissingYieldsSentinel(t *testing.T) {
	svc := newService(newFakeConfigRepo())

	if _, err := svc.FindByKey(context.Background(), "sys.absent"); !errors.Is(err, param.ErrConfigNotFound) {
		t.Fatalf("got %v, want ErrConfigNotFound", err)
	}
}
