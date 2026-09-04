package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OperLog holds the schema for the Oper Log entity (操作日志), ported from
// RuoYi sys_oper_log: the monitoring record of one mutating operation.
// One row per recorded request, written best-effort by the transport
// middleware (ADR 0012); rows are write-once — withdrawal (soft delete) is
// the only change a row ever takes. RuoYi's title/method/oper_url collapse
// into the kratos operation name; the operator is denormalized (id + name)
// so history survives account withdrawal.
type OperLog struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002). oper_user_id carries the
// operator separately from create_by's mechanics: 0 = an unauthenticated
// actor (recorded before auth succeeded, e.g. a rejected write).
func (OperLog) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the OperLog.
func (OperLog) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the
		// repo on create (see the audit package's newSnowflakeID).
		field.Int64("id").Unique().Immutable(),
		// Kratos operation name (e.g. "/iam.v1.UserService/CreateUser").
		field.String("operation"),
		field.String("http_method").Default(""),
		field.Int64("oper_user_id").Default(0),
		field.String("oper_user_name").Default(""),
		field.String("ipaddr").Default(""),
		// Redacted request payload as protojson (secret keys scrubbed —
		// ADR 0012); a placeholder when marshaling itself fails.
		field.String("request").Default(""),
		// 0 success, 1 failure (RuoYi status).
		field.Int8("status").Default(0).SchemaType(map[string]string{
			dialect.Postgres: "smallint",
		}),
		// Kratos error reason; empty on success.
		field.String("reason").Default(""),
		field.String("error_msg").Default(""),
		field.Int64("latency_ms").Default(0),
		// Request correlation with the structured request log.
		field.String("request_id").Default(""),
	}
}

// Annotations pins the table name to RuoYi's sys_oper_log.
func (OperLog) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_oper_log"},
	}
}

// Indexes serves the monitor page's reads: newest-first scans and
// per-operator lookups.
func (OperLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
		index.Fields("oper_user_id"),
	}
}
