package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PayBusinessNotice holds the schema for a Business Notification (业务通知).
// It is not the Channel Notification evidence log. One payment notice per
// Payment, and one refund notice per Refund, among rows that are not
// withdrawn. The address and the Business Order Number are copied when the
// notice is recorded. Delivery attempts are a separate record and are not
// this table.
type PayBusinessNotice struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (PayBusinessNotice) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the PayBusinessNotice.
func (PayBusinessNotice) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		// Pay Application the Payment belonged to when the notice was recorded.
		field.Int64("application_id"),
		// 1 payment, 2 refund.
		field.Int16("kind"),
		field.Int64("payment_id"),
		// Set when kind is refund.
		field.Int64("refund_id").Optional().Nillable(),
		// Business Order Number, copied for the payment body.
		field.String("biz_no").MaxLen(64),
		// Result address copied for automatic delivery. Never empty: an
		// empty copied address does not insert a row.
		field.String("notify_url").MaxLen(512),
		// 1 waiting, 2 accepted, 3 exhausted.
		field.Int16("status"),
		// Completed automatic attempts. The first attempt has not run yet
		// when the row is inserted.
		field.Int("attempt_count").Default(0),
		// When the next automatic attempt is due. Set when the notice is
		// waiting; a scanner uses it. Null once the notice is no longer waiting.
		field.Time("next_attempt_at").Optional().Nillable(),
	}
}

// Annotations pins the table name to pay_business_notice.
func (PayBusinessNotice) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "pay_business_notice"},
	}
}

// Indexes keep one active notice per paid Payment and one per finished
// Refund. Withdrawn rows free the pair.
func (PayBusinessNotice) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kind", "payment_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND kind = 1")),
		index.Fields("kind", "refund_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL AND kind = 2")),
	}
}
