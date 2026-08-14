package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// User holds the schema for the system-user entity (ported from RuoYi sys_user).
type User struct {
	ent.Schema
}

// Fields of the User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the RuoYi-style user_id column. Set by the repo
		// on create (see data.newSnowflakeID).
		field.Int64("id").StorageKey("user_id").Unique().Immutable(),
		// Uniqueness is enforced in code (biz checks against active rows), not
		// by DB constraints — repo-wide convention (see .scratch/role/plan.md).
		// Soft-deleted values are reusable.
		field.String("user_name"),
		field.String("nick_name").Default(""),
		// Nullable: NULL when unset; uniqueness enforced in code when set.
		field.String("email").Optional().Nillable(),
		field.String("phone_number").Optional().Nillable(),
		// smallint in PG (RuoYi's char(1) enums made numeric).
		field.Int8("gender").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		field.Int8("status").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		field.String("password").Default(""),
		field.String("avatar").Optional().Default("").MaxLen(256),
		field.String("remark").Optional().Default(""),
		// Audit; written by the auth module (0 until then).
		field.Int64("create_by").Optional().Default(0),
		field.Int64("update_by").Optional().Default(0),
		// Last-login fields; written by the auth module, read-only here.
		field.String("login_ip").Optional().Default(""),
		field.Time("login_date").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		// Soft delete (replaces RuoYi del_flag). NULL = not deleted.
		field.Time("deleted_at").Optional().Nillable(),
	}
}

// Annotations pins the table name to RuoYi's sys_user (ent would otherwise
// default to "users").
func (User) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_user"},
	}
}
