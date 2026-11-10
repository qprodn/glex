package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PayBusinessNoticeAttempt holds one delivery try for a Business
// Notification. Automatic attempts set manual false. http_status is empty
// when no response arrived. accepted is HTTP 2xx. The response body is
// not stored.
type PayBusinessNoticeAttempt struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (PayBusinessNoticeAttempt) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the PayBusinessNoticeAttempt.
func (PayBusinessNoticeAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		field.Int64("notice_id"),
		// False for an automatic attempt, true for a manual resend.
		field.Bool("manual"),
		// Empty when the call produced no HTTP response.
		field.Int("http_status").Optional().Nillable(),
		field.String("error").MaxLen(512).Default(""),
		// True when the response status was HTTP 2xx.
		field.Bool("accepted"),
	}
}

// Annotations pins the table name to pay_business_notice_attempt.
func (PayBusinessNoticeAttempt) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "pay_business_notice_attempt"},
	}
}

// Indexes serve the read of one notice's attempts. Withdrawn rows drop out.
func (PayBusinessNoticeAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("notice_id").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
