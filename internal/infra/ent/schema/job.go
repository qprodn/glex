package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Job holds the schema for the Scheduled Job entity (定时任务, ADR 0023) —
// designed natively for glex rather than ported: no Quartz, no
// invoke_target strings. The handler column names a code-registered Job
// Executor; the cron column is a 5-field expression evaluated in the one
// configured scheduler timezone; enabled is the pause/resume switch.
type Job struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (Job) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the Job.
func (Job) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the repo on create
		// (see jobinfra.newSnowflakeID).
		field.Int64("id").Unique().Immutable(),
		field.String("name").MaxLen(64),
		// Registry key of the Job Executor this job fires. Validated against
		// the live registry at save time (ADR 0023) — the row never carries
		// executable text.
		field.String("handler").MaxLen(128),
		// 5-field cron expression (robfig ParseStandard syntax, descriptors
		// included). Evaluated in conf.Job.timezone on every replica.
		field.String("cron").MaxLen(64),
		// Opaque executor argument, passed verbatim to Run.
		field.Text("args").Default(""),
		field.Bool("enabled").Default(true),
		// Per-run timeout; 1–3600. The row's default is the config default
		// resolved at create time, so later config changes leave existing
		// jobs explicit. The Min/Max run in the generated builders as
		// defense-in-depth behind the domain service's validation.
		field.Int32("timeout_seconds").Default(60).
			Min(1).Max(3600).
			SchemaType(map[string]string{dialect.Postgres: "integer"}),
		field.String("remark").Default("").MaxLen(255),
	}
}

// Annotations pins the table name (ent would otherwise default to "jobs").
func (Job) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "job"},
	}
}

// Indexes enforces at most one active row per name (withdrawn rows free the
// name) and serves the by-handler console filter. The scheduler's
// ListEnabled scan walks the whole (small) active set — no extra index.
func (Job) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("handler").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}

// JobLog holds the schema for the Job Log entity (任务日志, ADR 0023): the
// write-once record of one completed firing. There is no "running" status —
// the row appears only at completion — and no endpoint ever updates a row;
// withdrawal (soft delete) is the only change.
type JobLog struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002). create_by/update_by carry
// 0: firings are system-driven, manual included — the operator asked, the
// scheduler ran.
func (JobLog) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the JobLog.
func (JobLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		// Bare reference to the job row; no FK. The job's withdrawal keeps
		// its logs readable as history.
		field.Int64("job_id"),
		// Snapshot of the job's handler at fire time — a later handler
		// change never rewrites history.
		field.String("handler").MaxLen(128),
		// 1 scheduled, 2 manual, 3 external (reserved, ADR 0023 replacement
		// path).
		field.Int16("trigger"),
		// 1 success, 2 failure.
		field.Int16("status"),
		field.Text("message").Default(""),
		field.Time("started_at"),
		field.Int64("duration_ms"),
	}
}

// Annotations pins the table name (ent would otherwise default to
// "job_logs").
func (JobLog) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "job_log"},
	}
}

// Indexes serves the two read paths: one job's recent runs, and the
// all-jobs recent-runs console list.
func (JobLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("job_id", "created_at").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("created_at"),
	}
}
