package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Role holds the schema for the role entity (ported from RuoYi sys_role).
// Basic CRUD only — no associations with other tables.
type Role struct {
	ent.Schema
}

// Fields of the Role.
func (Role) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the RuoYi-style role_id column. Set by the
		// repo on create (see data.newSnowflakeID).
		field.Int64("id").StorageKey("role_id").Unique().Immutable(),
		// Uniqueness is enforced in code (biz checks against active rows), not
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
		field.String("remark").Default(""),
		// Audit; written by the auth module (0 until then).
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
