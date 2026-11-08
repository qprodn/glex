package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WechatPlatform holds one WeChat Platform registration (微信平台). The
// component secret, Token, and EncodingAESKey are one encrypted JSON blob
// (AES-256-GCM under WECHAT_MASTER_KEY, ADR 0018 / ADR 0029). The mask
// columns are the write-time echo every display path serves — displaying
// never decrypts. The component AppID is immutable and, among rows that
// are not withdrawn, identifies one platform.
type WechatPlatform struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (WechatPlatform) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the WechatPlatform.
func (WechatPlatform) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the repo on create.
		field.Int64("id").Unique().Immutable(),
		field.String("name").MaxLen(64),
		// Immutable for the life of the row (ADR 0029).
		field.String("component_app_id").MaxLen(64).Immutable(),
		// Secrets JSON, encrypted whole. Format:
		// key_version:nonce_base64:ciphertext_base64.
		field.Text("secret_cipher"),
		field.String("component_secret_mask").MaxLen(16),
		field.String("token_mask").MaxLen(16),
		field.String("encoding_aes_key_mask").MaxLen(16),
		// Master-key version (KEK rotation reserve, ADR 0018).
		field.Int("key_version").Default(1),
		// Current verify ticket, encrypted whole. Empty means the platform
		// does not hold one, so it is not ready. Display paths never
		// decrypt this column and never return the plaintext.
		field.Text("verify_ticket_cipher").Default(""),
	}
}

// Annotations pins the table name to wechat_platform (wechat is not of
// RuoYi lineage — no sys_ prefix).
func (WechatPlatform) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "wechat_platform"},
	}
}

// Indexes enforces at most one live platform per component AppID;
// withdrawn rows free the AppID for a new row (ADR 0002, ADR 0029).
func (WechatPlatform) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("component_app_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
