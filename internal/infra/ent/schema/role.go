package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Role holds the schema for the role entity (ported from RuoYi sys_role).
// CRUD + the role↔menu link (Menu Grant) via the sys_role_menu link rows.
type Role struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (Role) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the Role.
func (Role) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the
		// repo on create (see data.newSnowflakeID).
		field.Int64("id").Unique().Immutable(),
		// Uniqueness is enforced in code (domain checks against active rows), not
		// by DB constraints — repo-wide convention (see .scratch/role/plan.md).
		// Soft-deleted values are reusable.
		field.String("role_name"),
		field.String("role_key"),
		field.Int("role_sort").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "integer",
		}),
		// smallint in PG (RuoYi's char(1) status made numeric).
		field.Int8("status").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		// 菜单树选择项是否关联显示 (RuoYi menu_check_strictly, default 1). Default
		// on; shapes how the role editor presents the menu tree, not what is
		// stored in sys_role_menu (grants are always stored verbatim).
		field.Bool("menu_check_strictly").Default(true),
		field.String("remark").Default(""),
	}
}

// Annotations pins the table name to RuoYi's sys_role (ent would otherwise
// default to "roles").
func (Role) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_role"},
	}
}

// Edges of the Role. The Menu Grant (role↔menu) and the Role Assignment's
// by-role side (user↔role) live as explicit link entities — sys_role_menu and
// sys_user_role rows with audit columns and soft delete (ADR 0002).
func (Role) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("menu_grants", RoleMenu.Type),
		edge.To("user_links", UserRole.Type),
	}
}
