package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// User holds the schema for the system-user entity (ported from RuoYi sys_user).
type User struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (User) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the RuoYi-style user_id column. Set by the repo
		// on create (see data.newSnowflakeID).
		field.Int64("id").StorageKey("user_id").Unique().Immutable(),
		// Uniqueness is enforced in code (domain checks against active rows), not
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
		// The credential column: read and written only by authn's adapters
		// (ADR 0016); it stays on this table until a service split forces a
		// dedicated credential store.
		field.String("password").Default(""),
		field.String("avatar").Optional().Default("").MaxLen(256),
		field.String("remark").Optional().Default(""),
		// Last-login fields; written by the authn module, read-only here.
		field.String("login_ip").Optional().Default(""),
		field.Time("login_date").Optional().Nillable(),
	}
}

// Annotations pins the table name to RuoYi's sys_user (ent would otherwise
// default to "users").
func (User) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_user"},
	}
}

// Edges of the User. The Role Assignment (角色分配) lives in sys_user_role as
// explicit UserRole link rows — one per assignment, audit columns and soft
// delete included (ADR 0002) — owned by the access module, not the user
// module's concern.
func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("role_assignments", UserRole.Type),
	}
}
