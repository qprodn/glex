package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WechatAdoptedTemplate is one template taken from WeChat's public
// library onto one WeChat App. This module does not invent templates.
// The same library template is not adopted twice onto the same live app.
// Withdrawing the app does not withdraw the template. Withdrawal of a
// template row, if it ever happens, sets deleted_at (ADR 0002).
type WechatAdoptedTemplate struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (WechatAdoptedTemplate) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the WechatAdoptedTemplate.
func (WechatAdoptedTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		field.Int64("app_id"),
		field.String("library_template_id").MaxLen(128),
		field.String("wechat_template_id").MaxLen(128).Default(""),
		field.String("title").MaxLen(128).Default(""),
		// template_message, subscribe_notification, or subscribe_message.
		field.String("channel").MaxLen(64),
	}
}

// Annotations pins the table name.
func (WechatAdoptedTemplate) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "wechat_adopted_template"},
	}
}

// Indexes keeps one live adoption of a library template per app.
func (WechatAdoptedTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("app_id", "library_template_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("app_id"),
	}
}
