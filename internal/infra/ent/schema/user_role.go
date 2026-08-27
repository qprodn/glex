package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserRole holds the schema for the user↔role link rows — one Role Assignment
// row in sys_user_role (用户和角色关联表, ported from RuoYi). It is an explicit
// entity, not an ent M2M edge table: every table carries the audit columns and
// soft-deletes its rows (ADR 0002), which edge tables cannot do. Owned by the
// access module; the user module never reads it.
type UserRole struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (UserRole) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the UserRole. Snowflake id; a re-assigned pair is a new row, so
// the pair itself cannot be the PK.
func (UserRole) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("user_role_id").Unique().Immutable(),
		field.Int64("user_id"),
		field.Int64("role_id"),
	}
}

// Annotations pins the table to RuoYi's sys_user_role.
func (UserRole) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_user_role"},
	}
}

// Indexes enforces at most one active row per (user_id, role_id) pair;
// withdrawn generations coexist under the surrogate PK (ADR 0002).
func (UserRole) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "role_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}

// Edges of the UserRole: both endpoints are required; the FK columns are
// field-backed. Assigning is full-replacement at the set level — the repo
// soft-deletes the user's active rows and inserts the new set (ADR 0002).
func (UserRole) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("role_assignments").
			Field("user_id").
			Unique().
			Required(),
		edge.From("role", Role.Type).
			Ref("user_links").
			Field("role_id").
			Unique().
			Required(),
	}
}
