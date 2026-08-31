package entity

import (
	"time"

	"glex/internal/domain/dict"
)

// DictType is a Dict Type domain object (字典类型), ported from RuoYi
// sys_dict_type. The key identifies the vocabulary to every consumer and is
// immutable once created (ADR 0006); only the display name and remark change.
type DictType struct {
	ID        int64
	DictName  string
	DictType  string
	Remark    string
	CreateBy  int64
	UpdateBy  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate enforces the invariants a DictType must satisfy before
// persistence: a non-blank display name and a key matching the RuoYi
// dictionary-type charset.
func (t *DictType) Validate() error {
	if t == nil || t.DictName == "" || !dict.TypeKeyPattern.MatchString(t.DictType) {
		return dict.ErrDictTypeInvalid
	}
	return nil
}

// DictEntry is a Dict Entry domain object (字典项), ported from RuoYi
// sys_dict_data. The owning type key is set at create and never moves
// (ADR 0006); (DictType, DictValue) is unique among active entries.
type DictEntry struct {
	ID        int64
	DictType  string
	DictLabel string
	DictValue string
	DictSort  int
	TagStyle  string
	IsDefault bool
	Remark    string
	CreateBy  int64
	UpdateBy  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate enforces the invariants a DictEntry must satisfy before
// persistence: non-blank label and value, and a known tag color preset.
func (e *DictEntry) Validate() error {
	if e == nil || e.DictLabel == "" || e.DictValue == "" || !dict.TagStyles[e.TagStyle] {
		return dict.ErrDictEntryInvalid
	}
	return nil
}
