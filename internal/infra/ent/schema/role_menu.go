package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RoleMenu holds the schema for the role↔menu link rows — one Menu Grant row
// in sys_role_menu (角色和菜单关联表, ported from RuoYi). It is an explicit
// entity, not an ent M2M edge table: every table carries the audit columns and
// soft-deletes its rows (ADR 0002), which edge tables cannot do.
type RoleMenu struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (RoleMenu) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the RoleMenu. Snowflake id; a re-granted pair is a new row, so the
// pair itself cannot be the PK.
func (RoleMenu) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("role_menu_id").Unique().Immutable(),
		field.Int64("role_id"),
		field.Int64("menu_id"),
	}
}

// Annotations pins the table to RuoYi's sys_role_menu.
func (RoleMenu) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_role_menu"},
	}
}

// Indexes enforces at most one active row per (role_id, menu_id) pair;
// withdrawn generations coexist under the surrogate PK (ADR 0002).
func (RoleMenu) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("role_id", "menu_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}

// Edges of the RoleMenu: both endpoints are required; the FK columns are
// field-backed. Granting is full-replacement at the set level — the repo
// soft-deletes the role's active rows and inserts the new set (ADR 0002).
func (RoleMenu) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("role", Role.Type).
			Ref("menu_grants").
			Field("role_id").
			Unique().
			Required(),
		edge.From("menu", Menu.Type).
			Ref("role_links").
			Field("menu_id").
			Unique().
			Required(),
	}
}
