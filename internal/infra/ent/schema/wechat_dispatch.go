package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WechatDispatch is one attempt to send an Adopted Template to one
// openid. Acceptance and business rejection are each a row. A timeout
// is not a row. Sending again inserts another row. Withdrawing the app
// does not withdraw the dispatch (ADR 0002). The filled content is not
// a secret.
type WechatDispatch struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (WechatDispatch) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the WechatDispatch.
func (WechatDispatch) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		field.Int64("app_id"),
		field.String("openid").MaxLen(64),
		field.Int64("template_id"),
		field.String("library_template_id").MaxLen(128).Default(""),
		field.String("wechat_template_id").MaxLen(128).Default(""),
		field.String("channel").MaxLen(64).Default(""),
		field.Text("content").Default(""),
		field.String("web_url").MaxLen(1024).Default(""),
		field.String("mini_program_app_id").MaxLen(64).Default(""),
		field.String("mini_program_page").MaxLen(1024).Default(""),
		field.Bool("accepted").Default(false),
		field.String("msgid").MaxLen(64).Default(""),
		// Delivered is a later delivery report for this msgid. Acceptance
		// is not delivery. An unknown msgid does not insert a row.
		field.Bool("delivered").Default(false),
	}
}

// Annotations pins the table name.
func (WechatDispatch) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "wechat_dispatch"},
	}
}

// Indexes support the dispatch list, filtered by app, and a later
// delivery report that looks up an msgid.
func (WechatDispatch) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("app_id"),
		index.Fields("msgid"),
	}
}
