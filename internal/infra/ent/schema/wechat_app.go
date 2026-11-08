package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"glex/internal/domain/wechat/entity"
)

// WechatApp holds one WeChat App (微信应用). A Direct Hold stores the
// AppSecret and the generated Push Secret as one encrypted JSON blob
// (AES-256-GCM under WECHAT_MASTER_KEY, ADR 0018 / ADR 0029). A Platform
// Authorization stores the authorizer refresh material in that same blob
// and no authorizer AppSecret. The mask columns are the write-time echo
// every display path serves — displaying never decrypts. The WeChat
// AppID, the connection, and the platform binding are immutable. Among
// rows that are not withdrawn, one AppID identifies one app, which is a
// different uniqueness rule from a platform's component AppID. Suspension
// is a flag; withdrawal sets deleted_at (ADR 0002). Lapse is a separate
// flag, set only when WeChat definitively refuses the app.
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
		// direct_hold or platform_authorization. Immutable (ADR 0029).
		field.String("connection").MaxLen(32).Immutable(),
		// 0 for a Direct Hold. The WeChat Platform a Platform Authorization
		// is bound to. Immutable for the life of the row (ADR 0029).
		field.Int64("platform_id").Default(0).Immutable(),
		// Names of the capabilities WeChat granted. Empty for a Direct
		// Hold. Not WeChat permission-set numbers.
		field.JSON("granted_capabilities", []string{}).Optional(),
		// Secrets JSON, encrypted whole. Format:
		// key_version:nonce_base64:ciphertext_base64.
		field.Text("secret_cipher"),
		field.String("app_secret_mask").MaxLen(16),
		field.String("token_mask").MaxLen(16),
		field.String("encoding_aes_key_mask").MaxLen(16),
		// Operator switch. Not withdrawal and not lapse.
		field.Bool("suspended").Default(false),
		// Set when WeChat definitively refuses the grant or the Direct
		// Hold secret. Cleared by a replacement AppSecret or a new grant
		// on the same platform.
		field.Bool("lapsed").Default(false),
		// Where the browser lands after Web Authorization. Empty until an
		// operator saves one on a Service Account. A request cannot
		// substitute another address.
		field.String("return_url").MaxLen(2048).Default(""),
		// openid, or profile when the app also asks for nickname and
		// avatar. A new Service Account starts at openid.
		field.String("web_auth_scope").MaxLen(32).Default("openid"),
		// The Custom Menu saved in this system. Writing it does not call
		// WeChat. Null until an operator saves one.
		field.JSON("saved_menu", &entity.CustomMenu{}).Optional(),
		// The last menu WeChat accepted as this Service Account's default
		// menu. A refused or timed-out publish leaves it unchanged.
		field.JSON("accepted_menu", &entity.CustomMenu{}).Optional(),
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
		index.Fields("platform_id"),
	}
}
