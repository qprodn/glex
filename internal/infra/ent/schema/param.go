package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Param holds the schema for the Runtime Parameter entity (运行时参数),
// ported from RuoYi sys_config. The schema type is Param, not Config — ent
// reserves "config" as a predeclared identifier — mirroring the module's
// Go-name param / table-name sys_config split. Unlike a Dict Type key,
// config_key stays editable after create (see docs/modules/param.md): it is
// referenced only by consumer code, never by rows, so a rename surfaces as a
// consumer-side not-found instead of dangling data.
type Param struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (Param) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the Config.
func (Param) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the
		// repo on create (see data.newSnowflakeID).
		field.Int64("id").Unique().Immutable(),
		// Name and key carry the domain's 256-char budgets (param.NameMaxLen /
		// KeyMaxLen); the explicit SchemaType keeps ent's DDL in step with
		// docs/db/schema.sql and the validator instead of its varchar(255)
		// default — a budget-length value must never fail the insert.
		field.String("config_name").
			SchemaType(map[string]string{dialect.Postgres: "varchar(256)"}),
		// Parameter key (^[A-Za-z0-9][A-Za-z0-9._-]*$). Uniqueness among
		// active rows is backed by the partial unique index below — the exact
		// same DB guarantee the dict key carries, since consumer code resolves
		// parameters by this identifier; the friendly pre-check lives in the
		// domain service. Soft-deleted keys are reusable.
		field.String("config_key").
			SchemaType(map[string]string{dialect.Postgres: "varchar(256)"}),
		// Plain string value; varchar(500) keeps RuoYi's headroom for values
		// that carry data rather than labels (the repo's 256 default is a
		// label-sized budget).
		field.String("config_value").Default("").
			SchemaType(map[string]string{dialect.Postgres: "varchar(500)"}),
		// Built-in Parameter flag (RuoYi config_type Y/N, normalized to the
		// repo's smallint switch convention like is_frame/visible): 1 = the
		// system assumes the parameter exists and withdrawal is refused.
		field.Int("built_in").Default(0).
			SchemaType(map[string]string{dialect.Postgres: "smallint"}),
		// Remark rides the same 500 budget as the value (param.RemarkMaxLen).
		field.String("remark").Default("").
			SchemaType(map[string]string{dialect.Postgres: "varchar(500)"}),
	}
}

// Annotations pins the table name to RuoYi's sys_config (ent would otherwise
// default to "configs").
func (Param) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_config"},
	}
}

// Indexes enforces at most one active row per key; withdrawn rows free the
// key for reuse (ADR 0002 soft delete). The same index serves the
// ConfigReader's by-key point reads.
func (Param) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("config_key").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
