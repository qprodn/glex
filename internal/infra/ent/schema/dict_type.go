package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DictType holds the schema for the Dict Type entity (字典类型), ported from
// RuoYi sys_dict_type. The key (dict_type) is immutable once created
// (ADR 0006). Entries reference the type by key — sys_dict_data carries no
// FK to this table (RuoYi style), so there are no edges.
type DictType struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (DictType) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the DictType.
func (DictType) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the
		// repo on create (see data.newSnowflakeID).
		field.Int64("id").Unique().Immutable(),
		field.String("dict_name").MaxLen(256),
		// Vocabulary key (^[a-z][a-z0-9_]*$), immutable after create
		// (ADR 0006). Uniqueness among active rows is backed by the partial
		// unique index below — a deliberate DB guarantee (the role module's
		// code-only check does not scale to an exact-match identifier that
		// every consumer references); the friendly pre-check lives in the
		// domain service. Soft-deleted keys are reusable.
		field.String("dict_type").MaxLen(256),
		field.String("remark").Default("").MaxLen(256),
	}
}

// Annotations pins the table name to RuoYi's sys_dict_type (ent would
// otherwise default to "dict_types").
func (DictType) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_dict_type"},
	}
}

// Indexes enforces at most one active row per key; withdrawn rows free the
// key for reuse (ADR 0002 soft delete).
func (DictType) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("dict_type").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
