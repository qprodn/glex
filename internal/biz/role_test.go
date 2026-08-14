package biz

import (
	"context"
	"testing"

	"github.com/go-kratos/kratos/v3/errors"
)

// fakeRoleRepo is an in-memory RoleRepo for testing the usecase in isolation
// from the data layer.
type fakeRoleRepo struct {
	byID    map[int64]*Role
	byName  map[string]*Role
	byKey   map[string]*Role
	nextID  int64
	created []*Role
	deleted []int64
}

func newFakeRoleRepo() *fakeRoleRepo {
	return &fakeRoleRepo{
		byID:   make(map[int64]*Role),
		byName: make(map[string]*Role),
		byKey:  make(map[string]*Role),
		nextID: 1,
	}
}

func (r *fakeRoleRepo) FindByID(_ context.Context, id int64) (*Role, error) {
	ro, ok := r.byID[id]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) FindByRoleName(_ context.Context, roleName string) (*Role, error) {
	ro, ok := r.byName[roleName]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) FindByRoleKey(_ context.Context, roleKey string) (*Role, error) {
	ro, ok := r.byKey[roleKey]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return ro, nil
}

func (r *fakeRoleRepo) ListRoles(context.Context, ...ListOption) ([]*Role, error) {
	return nil, nil
}

func (r *fakeRoleRepo) CreateRole(_ context.Context, ro *Role) (*Role, error) {
	ro.ID = r.nextID
	r.nextID++
	r.byID[ro.ID] = ro
	r.byName[ro.RoleName] = ro
	r.byKey[ro.RoleKey] = ro
	r.created = append(r.created, ro)
	return ro, nil
}

func (r *fakeRoleRepo) UpdateRole(_ context.Context, ro *Role) (*Role, error) {
	r.byID[ro.ID] = ro
	return ro, nil
}

func (r *fakeRoleRepo) DeleteRole(_ context.Context, id int64) error {
	if _, ok := r.byID[id]; !ok {
		return ErrRoleNotFound
	}
	r.deleted = append(r.deleted, id)
	return nil
}

func TestCreateRole_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo)

	got, err := uc.CreateRole(context.Background(), &Role{RoleName: "超级管理员", RoleKey: "superadmin", RoleSort: 1})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected a server-assigned id, got 0")
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created role, got %d", len(repo.created))
	}
}

func TestCreateRole_BlankNameRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo)

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "", RoleKey: "superadmin"})
	if !errors.Is(err, ErrRoleInvalid) {
		t.Fatalf("expected ErrRoleInvalid, got %v", err)
	}
}

func TestCreateRole_BlankKeyRejected(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo)

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "超级管理员", RoleKey: ""})
	if !errors.Is(err, ErrRoleInvalid) {
		t.Fatalf("expected ErrRoleInvalid, got %v", err)
	}
}

func TestCreateRole_NameConflict(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byName["超级管理员"] = &Role{ID: 7, RoleName: "超级管理员", RoleKey: "other"}
	uc := NewRoleUsecase(repo)

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "超级管理员", RoleKey: "superadmin"})
	if !errors.Is(err, ErrRoleNameConflict) {
		t.Fatalf("expected ErrRoleNameConflict, got %v", err)
	}
}

func TestCreateRole_KeyConflict(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byKey["superadmin"] = &Role{ID: 7, RoleName: "other", RoleKey: "superadmin"}
	uc := NewRoleUsecase(repo)

	_, err := uc.CreateRole(context.Background(), &Role{RoleName: "超级管理员", RoleKey: "superadmin"})
	if !errors.Is(err, ErrRoleKeyConflict) {
		t.Fatalf("expected ErrRoleKeyConflict, got %v", err)
	}
}

func TestUpdateRole_ConflictExcludingSelf(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[5] = &Role{ID: 5, RoleName: "管理员", RoleKey: "admin"}
	repo.byName["管理员"] = repo.byID[5]
	repo.byKey["admin"] = repo.byID[5]
	repo.byName["超级管理员"] = &Role{ID: 7, RoleName: "超级管理员", RoleKey: "superadmin"}
	uc := NewRoleUsecase(repo)

	// Taking another role's name is rejected...
	_, err := uc.UpdateRole(context.Background(), &Role{ID: 5, RoleName: "超级管理员", RoleKey: "admin"})
	if !errors.Is(err, ErrRoleNameConflict) {
		t.Fatalf("expected ErrRoleNameConflict, got %v", err)
	}
	// ...but keeping its own values passes.
	_, err = uc.UpdateRole(context.Background(), &Role{ID: 5, RoleName: "管理员", RoleKey: "admin"})
	if err != nil {
		t.Fatalf("update keeping own values: %v", err)
	}
}

func TestDeleteRole_Success(t *testing.T) {
	repo := newFakeRoleRepo()
	repo.byID[3] = &Role{ID: 3, RoleName: "测试", RoleKey: "test"}
	uc := NewRoleUsecase(repo)

	if err := uc.DeleteRole(context.Background(), 3); err != nil {
		t.Fatalf("delete role: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 3 {
		t.Fatalf("expected role 3 deleted, got %v", repo.deleted)
	}
}

func TestDeleteRole_NotFound(t *testing.T) {
	repo := newFakeRoleRepo()
	uc := NewRoleUsecase(repo)

	err := uc.DeleteRole(context.Background(), 99)
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}
