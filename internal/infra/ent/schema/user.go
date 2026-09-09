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
		// Snowflake ID; stored in the id column. Set by the repo
		// on create (see data.newSnowflakeID).
		field.Int64("id").Unique().Immutable(),
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
		// (ADR 0005); it stays on this table until a service split forces a
		// dedicated credential store.
		field.String("password").Default(""),
		// The user's avatar as a Stored File id (0 = none) — RuoYi's bigint
		// avatar column. Resolved to a Display URL on reads via the
		// crossdomain file contract (ADR 0016); no ent edge, no FK.
		field.Int64("avatar").Default(0),
		field.String("remark").Optional().Default(""),
		// The user's place in the org structure (ADR 0013): a plain column
		// referencing sys_dept, 0 = none. The org module validates it via the
		// crossdomain org contract; no ent edge and no FK across modules.
		field.Int64("dept_id").Default(0),
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
// module's concern. The Post Assignment (岗位分配) is the same shape in
// sys_user_post, owned by the org module.
func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("role_assignments", UserRole.Type),
		edge.To("post_assignments", UserPost.Type),
	}
}
