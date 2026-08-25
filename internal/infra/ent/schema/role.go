package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Role holds the schema for the role entity (ported from RuoYi sys_role).
// CRUD + the role↔menu link (Menu Grant) via the sys_role_menu join table.
type Role struct {
	ent.Schema
}

// Fields of the Role.
func (Role) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the RuoYi-style role_id column. Set by the
		// repo on create (see data.newSnowflakeID).
		field.Int64("id").StorageKey("role_id").Unique().Immutable(),
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
		// Audit; written by the access module (0 until then).
		field.Int64("create_by").Optional().Default(0),
		field.Int64("update_by").Optional().Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		// Soft delete (replaces RuoYi del_flag). NULL = not deleted.
		field.Time("deleted_at").Optional().Nillable(),
	}
}

// Annotations pins the table name to RuoYi's sys_role (ent would otherwise
// default to "roles").
func (Role) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_role"},
	}
}

// Edges of the Role. M2M to Menu via the join table sys_role_menu — exactly
// RuoYi's shape: columns role_id + menu_id, composite PK (role_id, menu_id),
// no timestamps/soft delete (hard-deleted rows, full-replacement semantics).
// The users edge is the inverse side of User.roles (Role Assignment); the
// join table sys_user_role is owned/named by the User schema's annotation.
func (Role) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("menus", Menu.Type).Annotations(
			entsql.Annotation{Table: "sys_role_menu"},
		),
		edge.From("users", User.Type).Ref("roles"),
	}
}
