package service

import (
	"context"
	"errors"

	"glex/internal/domain/dict"
	"glex/internal/domain/dict/entity"
	"glex/internal/domain/dict/repository"

	"go.einride.tech/aip/filtering"
	"go.einride.tech/aip/ordering"
)

// Dict is the dict domain service behind the dictionary endpoints: the only
// path to the Dict Type and Dict Entry reads and writes (ADR 0005). Methods
// join the caller's ambient transaction; the Application service owns the
// transaction boundary.
type Dict interface {
	GetType(ctx context.Context, id int64) (*entity.DictType, error)
	ListTypes(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.DictType, error)
	// Create validates the type and persists it.
	CreateType(ctx context.Context, t *entity.DictType) (*entity.DictType, error)
	// Update validates the change and persists it. The key is immutable
	// (ADR 0006): whatever the caller sends is replaced with the stored key
	// before the write.
	UpdateType(ctx context.Context, t *entity.DictType) (*entity.DictType, error)
	// DeleteType soft-deletes the type, refusing while entries are still
	// active under its key.
	DeleteType(ctx context.Context, id, actorID int64) error

	GetEntry(ctx context.Context, id int64) (*entity.DictEntry, error)
	ListEntries(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.DictEntry, error)
	// OptionsByType returns one type's active entries ordered by
	// (dict_sort, id) — the consumption read. An unknown key yields an
	// empty list, not an error.
	OptionsByType(ctx context.Context, key string) ([]*entity.DictEntry, error)
	// CreateEntry validates the entry against its owning type and persists
	// it.
	CreateEntry(ctx context.Context, e *entity.DictEntry) (*entity.DictEntry, error)
	// UpdateEntry validates the change and persists it. The owning type key
	// is immutable (ADR 0006): whatever the caller sends is replaced with
	// the stored key before the write.
	UpdateEntry(ctx context.Context, e *entity.DictEntry) (*entity.DictEntry, error)
	DeleteEntry(ctx context.Context, id, actorID int64) error
}

type dictImpl struct {
	types   repository.DictTypeRepo
	entries repository.DictEntryRepo
}

// NewDictService creates the dict domain service. types backs the type CRUD
// and the "entry under unknown type" guard; entries backs the entry CRUD,
// the value-uniqueness check, and the type-delete guard.
func NewDictService(types repository.DictTypeRepo, entries repository.DictEntryRepo) Dict {
	return &dictImpl{types: types, entries: entries}
}

// --- Dict Type (字典类型) ---

func (s *dictImpl) GetType(ctx context.Context, id int64) (*entity.DictType, error) {
	return s.types.FindByID(ctx, id)
}

func (s *dictImpl) ListTypes(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.DictType, error) {
	return s.types.List(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
		repository.ListLimit(limit),
		repository.ListOffset(offset),
	)
}

func (s *dictImpl) CreateType(ctx context.Context, t *entity.DictType) (*entity.DictType, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := s.assertTypeKeyAvailable(ctx, t.DictType); err != nil {
		return nil, err
	}
	return s.types.Create(ctx, t)
}

func (s *dictImpl) UpdateType(ctx context.Context, t *entity.DictType) (*entity.DictType, error) {
	stored, err := s.types.FindByID(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	// The key is immutable (ADR 0006): the stored key wins whatever the
	// caller sent, so the update can never drift the identifier — and never
	// needs a cascade.
	t.DictType = stored.DictType
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return s.types.Update(ctx, t)
}

func (s *dictImpl) DeleteType(ctx context.Context, id, actorID int64) error {
	stored, err := s.types.FindByID(ctx, id)
	if err != nil {
		return err
	}
	n, err := s.entries.CountByDictKey(ctx, stored.DictType)
	if err != nil {
		return err
	}
	if n > 0 {
		return dict.ErrDictTypeInUse
	}
	return s.types.Delete(ctx, id, actorID)
}

// --- Dict Entry (字典项) ---

func (s *dictImpl) GetEntry(ctx context.Context, id int64) (*entity.DictEntry, error) {
	return s.entries.FindByID(ctx, id)
}

func (s *dictImpl) ListEntries(ctx context.Context, filter filtering.Filter, orderBy ordering.OrderBy, limit, offset int) ([]*entity.DictEntry, error) {
	return s.entries.List(ctx,
		repository.ListFilter(filter),
		repository.ListOrderBy(orderBy),
		repository.ListLimit(limit),
		repository.ListOffset(offset),
	)
}

func (s *dictImpl) OptionsByType(ctx context.Context, key string) ([]*entity.DictEntry, error) {
	return s.entries.ListByDictKey(ctx, key)
}

func (s *dictImpl) CreateEntry(ctx context.Context, e *entity.DictEntry) (*entity.DictEntry, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.types.FindByKey(ctx, e.DictType); err != nil {
		if errors.Is(err, dict.ErrDictTypeNotFound) {
			return nil, dict.ErrDictTypeNotFound
		}
		return nil, err
	}
	if err := s.assertEntryValueAvailable(ctx, e.DictType, e.DictValue, 0); err != nil {
		return nil, err
	}
	return s.entries.Create(ctx, e)
}

func (s *dictImpl) UpdateEntry(ctx context.Context, e *entity.DictEntry) (*entity.DictEntry, error) {
	stored, err := s.entries.FindByID(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	// The owning type is immutable (ADR 0006): moving an entry between
	// types is withdraw-and-recreate, not an update.
	e.DictType = stored.DictType
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if err := s.assertEntryValueAvailable(ctx, e.DictType, e.DictValue, e.ID); err != nil {
		return nil, err
	}
	return s.entries.Update(ctx, e)
}

func (s *dictImpl) DeleteEntry(ctx context.Context, id, actorID int64) error {
	return s.entries.Delete(ctx, id, actorID)
}

// assertTypeKeyAvailable checks the key is not taken by an active type (ADR
// 0005: the check folds into the service, not a free function over the
// repo). A not-found from the repo is the expected "available" case. The
// check runs against active rows only, so a withdrawn type frees its key;
// the partial unique index remains the race-proof last line of defense.
func (s *dictImpl) assertTypeKeyAvailable(ctx context.Context, key string) error {
	if _, err := s.types.FindByKey(ctx, key); err != nil {
		if errors.Is(err, dict.ErrDictTypeNotFound) {
			return nil
		}
		return err
	}
	return dict.ErrDictTypeConflict
}

// assertEntryValueAvailable checks (dictType, value) is not taken by another
// active entry, ignoring the record with excludeID (0 = none). Soft-deleted
// values are reusable; the partial unique index is the last line of defense.
func (s *dictImpl) assertEntryValueAvailable(ctx context.Context, dictType, value string, excludeID int64) error {
	existing, err := s.entries.FindByTypeAndValue(ctx, dictType, value)
	if err != nil {
		if errors.Is(err, dict.ErrDictEntryNotFound) {
			return nil
		}
		return err
	}
	if existing != nil && existing.ID != excludeID {
		return dict.ErrDictEntryConflict
	}
	return nil
}
