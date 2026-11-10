package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PayApplication holds the schema for the Pay Application entity (支付应用).
// It is the caller's identity: its own number, a display name unique among
// rows that are not withdrawn, and exactly one Merchant Account id fixed at
// creation (ADR 0030). Result addresses may be empty. No app key, no secret,
// no edges, and no foreign keys — merchant_id is a bare bigint.
type PayApplication struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (PayApplication) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the PayApplication.
func (PayApplication) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the repo on create
		// (see internal/infra/pay snowflake.go). The number does not change.
		field.Int64("id").Unique().Immutable(),
		// Display name, unique among rows that are not withdrawn.
		field.String("name").MaxLen(64),
		// Merchant Account bound at creation. Not a foreign key.
		field.Int64("merchant_id"),
		// 0 enabled, 1 disabled.
		field.Int16("status").Default(0),
		// Payment result address. Empty means no payment notice is armed
		// at creation of a later Payment.
		field.String("order_notify_url").MaxLen(512).Default(""),
		// Refund result address. Empty is allowed.
		field.String("refund_notify_url").MaxLen(512).Default(""),
		field.String("remark").MaxLen(500).Default(""),
	}
}

// Annotations pins the table name to pay_application.
func (PayApplication) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "pay_application"},
	}
}

// Indexes: the display name is unique among rows that are not withdrawn,
// and live rows are looked up by the Merchant Account they bind.
func (PayApplication) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("merchant_id").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
