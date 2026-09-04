package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Dept holds the schema for the department entity (部门, ported from RuoYi
// sys_dept). The tree shape is parent_id only — no materialized ancestors
// path (ADR 0014); trees and subtrees are derived in the org domain service.
// RuoYi 6.0's dept_category is deliberately not ported.
type Dept struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (Dept) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the Dept. Snowflake id; leader is a System User id recorded for
// display (0 = none) and never validated against sys_user (RuoYi parity —
// the editor restricts the choices).
func (Dept) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		// Self-reference; 0 = root. No DB unique constraint on (parent_id,
		// dept_name) — sibling uniqueness is a soft domain check.
		field.Int64("parent_id").Default(0),
		field.String("dept_name"),
		field.Int("order_num").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "integer",
		}),
		field.Int64("leader").Default(0),
		field.String("phone").Default(""),
		field.String("email").Default(""),
		// smallint in PG (RuoYi's char(1) status made numeric); 0正常 1停用.
		field.Int8("status").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
	}
}

// Annotations pins the table to RuoYi's sys_dept.
func (Dept) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_dept"},
	}
}

// Indexes backs the children lookup behind the delete guard. Soft-deleted
// rows are filtered in code; no partial index needed for a plain lookup.
func (Dept) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("parent_id"),
	}
}

// Edges of the Dept: posts reference their department by dept_id (a plain
// column, like menu.parent_id — no ent edge and no FK across the org tables).
func (Dept) Edges() []ent.Edge {
	return nil
}
