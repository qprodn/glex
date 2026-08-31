package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DictData holds the schema for the Dict Entry rows (字典项), ported from
// RuoYi sys_dict_data. The domain noun is Dict Entry (CONTEXT.md renamed
// RuoYi's "dict data"); the table and columns keep RuoYi's names. The owning
// type is referenced by its key (dict_type, no FK — RuoYi style) and is set
// at create only (ADR 0006); css_class was dropped and status never existed
// in this fork.
type DictData struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (DictData) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the DictData.
func (DictData) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID (RuoYi's dict_code). Set by the repo on create.
		field.Int64("id").Unique().Immutable(),
		field.Int("dict_sort").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "integer",
		}),
		field.String("dict_label"),
		field.String("dict_value"),
		// Owning Dict Type key. Immutable after create (ADR 0006).
		field.String("dict_type"),
		// Tag color preset (default/primary/success/warning/danger/info),
		// validated in the domain; proto calls it tag_style.
		field.String("list_class").Default("default"),
		// Whether forms should preselect this option. Multiple defaults may
		// coexist (RuoYi semantics; RuoYi char Y/N made boolean).
		field.Bool("is_default").Default(false),
		field.String("remark").Default(""),
	}
}

// Annotations pins the table name to RuoYi's sys_dict_data.
func (DictData) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_dict_data"},
	}
}

// Indexes enforces at most one active entry per (dict_type, dict_value);
// withdrawn rows free the value for reuse (ADR 0002 soft delete). The
// composite also serves the by-type scan of the options endpoint.
func (DictData) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("dict_type", "dict_value").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
