package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PayRefund holds the schema for the Refund entity (退款单). The snowflake
// id doubles as the Refund number and the Channel's merchant refund number
// (out_refund_no). A Payment may carry several Refunds, together never
// exceeding what was paid — the cap is enforced in the application under
// the payment-scoped lock, and each refund ends refunded or failed.
type PayRefund struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (PayRefund) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the PayRefund.
func (PayRefund) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		field.Int64("payment_id"),
		// Copied from the payment at initiation — refunds are always
		// resolved on the channel the money arrived on.
		field.Int64("merchant_id"),
		field.Int16("channel"),
		// Refund result address copied from the Pay Application at creation.
		// Empty stays empty.
		field.String("notify_url").MaxLen(512).Default(""),
		// This refund's amount in minor units (ADR 0019).
		field.Int64("amount"),
		field.String("reason").MaxLen(256).Default(""),
		// 1 PENDING, 2 SUCCESS, 3 FAILED — CAS from PENDING, terminal
		// states irreversible (Alipay's synchronous refund settles
		// immediately through the same transition).
		field.Int16("status").Default(1),
		// Channel refund id, backfilled on success evidence.
		field.String("channel_refund_no").MaxLen(64).Optional(),
		field.String("fail_reason").MaxLen(256).Optional(),
		field.Time("refunded_at").Optional().Nillable(),
	}
}

// Annotations pins the table name to pay_refund.
func (PayRefund) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "pay_refund"},
	}
}

// Indexes serves the payment-detail read (all refunds of one payment) and
// the console list.
func (PayRefund) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("payment_id"),
	}
}
