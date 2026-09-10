package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PayPayment holds the schema for the Payment entity (支付单). The snowflake
// id doubles as the Payment number and the Channel's merchant order number
// (out_trade_no). Money is int64 minor units (ADR 0019); status advances by
// CAS from PENDING and terminal states are irreversible. Amounts are never
// converted in SQL — refunds accumulate refunded_amount by integer addition.
type PayPayment struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (PayPayment) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the PayPayment.
func (PayPayment) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		// Bare cross-module-style reference — the Merchant Account id; no FK.
		field.Int64("merchant_id"),
		// Snapshot of the Merchant Account's channel at initiation; a later
		// merchant withdrawal never re-points an existing payment.
		field.Int16("channel"),
		// Business Order Number: opaque caller reference, indexed, never
		// unique among Payments (a re-initiation closes the old pending row
		// and inserts a new one).
		field.String("biz_no").MaxLen(64),
		field.String("subject").MaxLen(256),
		// Minor units (fen for CNY) — `amount` is always minor units
		// (ADR 0019); formatting to yuan happens only at display edges.
		field.Int64("amount"),
		field.String("currency").MaxLen(3).Default("CNY"),
		// 1 Native, 2 JSAPI, 3 H5, 4 App (only Native in the MVP).
		field.Int16("pay_method").Default(1),
		// 1 PENDING, 2 PAID, 3 FAILED, 4 CLOSED, 5 EXPIRED.
		field.Int16("status").Default(1),
		// WeChat transaction_id / Alipay trade_no, backfilled by the
		// notification or query that proved the payment.
		field.String("channel_trade_no").MaxLen(64).Optional(),
		// Channel prepay response (code_url and kin) — lets the front end
		// re-render the QR without a fresh prepay.
		field.Text("channel_payload").Optional(),
		// openid / buyer_id, reported by the Channel on success.
		field.String("payer_id").MaxLen(64).Optional(),
		// Maintenance sum of successful refunds; only the refund flow adds
		// to it, and it never changes the status — paid and refunded are
		// two independent facts.
		field.Int64("refunded_amount").Default(0),
		field.String("fail_reason").MaxLen(256).Optional(),
		// Initiation time + the configured expiry window (default 30min).
		// Expiry is judged lazily on settlement paths, never by a cron.
		field.Time("expire_time"),
		field.Time("paid_at").Optional().Nillable(),
	}
}

// Annotations pins the table name to pay_payment.
func (PayPayment) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "pay_payment"},
	}
}

// Indexes serves the settlement paths: initiation looks up a merchant's
// pending rows per Business Order Number; the console filters by status and
// time; notifications and queries resolve by the channel's trade number.
// None of them is unique — re-initiation keeps history.
func (PayPayment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("merchant_id", "biz_no").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("status"),
		index.Fields("created_at"),
		index.Fields("channel_trade_no"),
	}
}
