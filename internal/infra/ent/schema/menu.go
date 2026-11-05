package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Menu holds the schema for the menu entity (ported from RuoYi sys_menu).
type Menu struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (Menu) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the Menu.
func (Menu) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the repo
		// on create (see data.newSnowflakeID).
		field.Int64("id").Unique().Immutable(),
		field.String("menu_name").MaxLen(256),
		// Self-reference; 0 = root. No DB unique constraint on (parent_id,
		// menu_name) — sibling uniqueness is a soft domain check.
		field.Int64("parent_id").Default(0),
		field.Int("order_num").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "integer",
		}),
		// Routing fields (path/component/query_param) are inert until the auth
		// module's getRouters consumes them; modeled now so getRouters reads the
		// same table with no later migration.
		field.String("path").Default("").MaxLen(256),
		field.String("component").Default("").MaxLen(256),
		field.String("query_param").Default("").MaxLen(256),
		// smallint in PG. is_frame/is_cache map RuoYi's Y/N to 0/1 (N=0, Y=1);
		// menu_type is the MenuType enum (1/2/3); visible/status are 0/1.
		field.Int8("is_frame").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		field.Int8("is_cache").Default(1).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		field.Int8("menu_type").SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		field.Int8("visible").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		field.Int8("status").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		field.String("perms").Default("").MaxLen(256),
		field.String("icon").Default("").MaxLen(256),
		field.String("active_menu").Default("").MaxLen(256),
		// RuoYi uses varchar(2000) here; schema.sql keeps varchar(256).
		field.String("ext").Default("").MaxLen(256),
	}
}

// Annotations pins the table name to RuoYi's sys_menu.
func (Menu) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_menu"},
	}
}

// Edges of the Menu. The by-menu side of the Menu Grant lives as explicit
// RoleMenu link rows in sys_role_menu, with audit columns and soft delete
// (ADR 0002).
func (Menu) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("role_links", RoleMenu.Type),
	}
}
