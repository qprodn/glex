package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// LoginLog holds the schema for the Login Log entity (登录日志), ported from
// RuoYi sys_logininfor (renamed sys_login_log — the sys_oss → sys_file
// precedent): the monitoring record of one sign-in, or of one Login Session
// ending (logout, admin 强退, self-revoke). authn writes it best-effort at
// the moment the outcome is known (ADR 0012); rows are write-once. The
// account is denormalized (id + name) so failed attempts for nonexistent
// accounts still record a name, and history survives account withdrawal.
type LoginLog struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002). create_by is the
// authenticated principal that caused the row: the signing-in user, the
// revoking administrator (强退), or 0 for unauthenticated attempts.
func (LoginLog) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the LoginLog.
func (LoginLog) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the
		// repo on create (see the audit package's newSnowflakeID).
		field.Int64("id").Unique().Immutable(),
		// The account the event is about; 0 when the attempt never
		// resolved to an account (wrong name, withdrawn user).
		field.Int64("user_id").Default(0),
		field.String("user_name").Default("").MaxLen(256),
		field.String("ipaddr").Default("").MaxLen(256),
		field.String("browser").Default("").MaxLen(256),
		field.String("os").Default("").MaxLen(256),
		// 0 success, 1 failure (RuoYi status).
		field.Int8("status").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		// Human-readable outcome ("登录成功", "密码错误", "强制退出").
		field.String("msg").Default("").MaxLen(256),
	}
}

// Annotations pins the table name (RuoYi sys_logininfor, renamed).
func (LoginLog) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_login_log"},
	}
}

// Indexes serves the monitor page's reads: newest-first scans and
// per-account lookups.
func (LoginLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
		index.Fields("user_id"),
	}
}
