package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PayNotifyLog holds the schema for the Channel Notification stream (渠道
// 回调流水) — write-once evidence per ADR 0020. Each accepted notification
// is recorded with its raw body and signature headers before any state
// advances; the (channel, notify_id) partial unique absorbs the channels'
// guaranteed redelivery, so a duplicate insert is not a second evidence
// row. The row sits outside the oper log: channels push without a Principal,
// so create_by is 0 (the system itself received it).
type PayNotifyLog struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (PayNotifyLog) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the PayNotifyLog.
func (PayNotifyLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		field.Int16("channel"),
		// The Channel's notification id (WeChat v3 Wechatpay-Notify-Id
		// header, Alipay notify_id) — the dedup half-key.
		field.String("notify_id").MaxLen(128),
		field.Int64("merchant_id"),
		// 1 payment, 2 refund.
		field.Int16("kind"),
		// The Payment or Refund number the notification asserts about.
		field.String("target_no").MaxLen(64),
		// Signature verification outcome — a false row is evidence of a
		// rejected delivery and never advances any state.
		field.Bool("verified"),
		// false when the insert lost the dedup race or verification failed;
		// true once the state advance ran.
		field.Bool("processed"),
		// The unmodified request body — the dispute anchor.
		field.Text("raw_body"),
		// The verification-relevant request headers, for forensics.
		field.JSON("sign_headers", map[string]string{}).Optional(),
		field.Time("received_at").Default(time.Now),
	}
}

// Annotations pins the table name to pay_notify_log.
func (PayNotifyLog) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "pay_notify_log"},
	}
}

// Indexes enforces at most one active notification per (channel, notify_id)
// — the dedup guarantee; withdrawn rows (there is no withdraw path today)
// would free the id. merchant_id serves merchant-scoped evidence reads.
func (PayNotifyLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel", "notify_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("merchant_id"),
	}
}
