package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// File holds the schema for the Stored File entity (存储文件), named sys_file
// here and continuing the lineage of RuoYi's sys_oss (renamed — see
// GLOSSARY.md). Each row pairs one accepted upload's metadata with the bytes
// in object storage; withdrawal soft-deletes the row and removes the bytes
// (ADR 0009). No FKs and no edges — the bytes live outside PostgreSQL.
type File struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (File) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the File.
func (File) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. The domain service mints it
		// before building the Stored Key so the key and the row correlate
		// (see internal/infra/file snowflake.go).
		field.Int64("id").Unique().Immutable(),
		field.String("original_name").MaxLen(256),
		// Lowercased extension without the dot ("" when none), derived from
		// the original name at accept time.
		field.String("suffix").Default("").MaxLen(16),
		// Content type sniffed from the bytes at accept time; a client's
		// declared Content-Type never reaches this column.
		field.String("mime_type").MaxLen(128),
		field.Int64("size_bytes"),
		// Bucket and Stored Key locate the bytes in object storage. No public
		// URL exists — bytes are addressable only through the service
		// (ADR 0008).
		field.String("bucket").MaxLen(128),
		field.String("obj_key").MaxLen(512),
		// SHA-256 hex digest of the bytes (the Content Fingerprint). Recorded
		// for integrity; never used to unify — identical bytes accepted twice
		// yield two Stored Files.
		field.String("content_hash").MaxLen(64),
	}
}

// Annotations pins the table name to sys_file.
func (File) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sys_file"},
	}
}

// Indexes keeps the Content Fingerprint queryable for a future integrity
// audit; deliberately NOT unique — no dedup exists (see GLOSSARY.md).
func (File) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("content_hash"),
	}
}
