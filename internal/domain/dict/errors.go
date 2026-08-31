// Package dict owns the dict module per ADR 0005: Dict Types and their Dict
// Entries — the labeled option vocabularies pages render selects and status
// tags from. The package root holds only the typed errors; the objects live
// in entity, the ports in repository, and the module's own rules in service.
package dict

import (
	"github.com/go-kratos/kratos/v3/errors"
)

var (
	// ErrDictTypeNotFound: the requested dict type does not exist (or is
	// soft-deleted).
	ErrDictTypeNotFound = errors.NotFound("DICT_TYPE_NOT_FOUND", "dict type not found")
	// ErrDictTypeConflict: the dict_type key already exists among active
	// types.
	ErrDictTypeConflict = errors.Conflict("DICT_TYPE_CONFLICT", "dict_type already exists")
	// ErrDictTypeInvalid: dict_name is blank, or dict_type is blank or
	// violates the key charset.
	ErrDictTypeInvalid = errors.BadRequest("DICT_TYPE_INVALID", "dict_name is required and dict_type must match ^[a-z][a-z0-9_]*$")
	// ErrDictTypeInUse: the type still has active entries and cannot be
	// deleted (RuoYi: 已分配，不能删除).
	ErrDictTypeInUse = errors.BadRequest("DICT_TYPE_IN_USE", "dict type has entries and cannot be deleted")
)

var (
	// ErrDictEntryNotFound: the requested entry does not exist (or is
	// soft-deleted).
	ErrDictEntryNotFound = errors.NotFound("DICT_ENTRY_NOT_FOUND", "dict entry not found")
	// ErrDictEntryConflict: the (dict_type, dict_value) pair already exists
	// among active entries (RuoYi: 字典键值已存在).
	ErrDictEntryConflict = errors.Conflict("DICT_ENTRY_CONFLICT", "dict_value already exists in this dict type")
	// ErrDictEntryInvalid: dict_label/dict_value is blank or tag_style is not
	// a valid preset.
	ErrDictEntryInvalid = errors.BadRequest("DICT_ENTRY_INVALID", "dict_label and dict_value are required and tag_style must be a valid preset")
)
