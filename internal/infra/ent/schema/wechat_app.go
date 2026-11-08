package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WechatApp holds one WeChat App (微信应用). A Direct Hold stores the
// AppSecret and the generated Push Secret as one encrypted JSON blob
// (AES-256-GCM under WECHAT_MASTER_KEY, ADR 0018 / ADR 0029). The mask
// columns are the write-time echo every display path serves — displaying
// never decrypts. The WeChat AppID and the connection are immutable.
// Among rows that are not withdrawn, one AppID identifies one app, which
// is a different uniqueness rule from a platform's component AppID.
// Suspension is a flag; withdrawal sets deleted_at (ADR 0002). This
// ticket does not store a lapse flag.
type WechatApp struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (WechatApp) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the WechatApp.
func (WechatApp) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the repo on create.
		field.Int64("id").Unique().Immutable(),
		field.String("name").MaxLen(64),
		// WeChat AppID. Immutable for the life of the row (ADR 0029).
		field.String("wx_app_id").MaxLen(64).Immutable(),
		// mini_program or official_account. Fixed for the life of the row.
		field.String("app_type").MaxLen(32).Immutable(),
		// service_account, subscription_account, or empty for a mini program.
		field.String("account_kind").MaxLen(32).Default(""),
		// direct_hold. Immutable for the life of the row (ADR 0029).
		field.String("connection").MaxLen(32).Immutable(),
		// Secrets JSON, encrypted whole. Format:
		// key_version:nonce_base64:ciphertext_base64.
		field.Text("secret_cipher"),
		field.String("app_secret_mask").MaxLen(16),
		field.String("token_mask").MaxLen(16),
		field.String("encoding_aes_key_mask").MaxLen(16),
		// Operator switch. Not withdrawal and not lapse.
		field.Bool("suspended").Default(false),
		// Master-key version (KEK rotation reserve, ADR 0018).
		field.Int("key_version").Default(1),
	}
}

// Annotations pins the table name to wechat_app (wechat is not of
// RuoYi lineage — no sys_ prefix).
func (WechatApp) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "wechat_app"},
	}
}

// Indexes enforces at most one live app per WeChat AppID; withdrawn rows
// free the AppID for a new row (ADR 0002, ADR 0029).
func (WechatApp) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("wx_app_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
