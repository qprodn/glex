package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PayMerchantAccount holds the schema for the Merchant Account entity (商户
// 账号), the platform's own collecting account at one Channel. The channel
// config is one encrypted JSON blob (ADR 0021) carried in config_cipher
// (AES-256-GCM under the env-held master key, ADR 0018); config_masked is
// the write-time masked echo served by every display path — displaying
// never decrypts. No FKs and no edges — cross-module references are bare
// int64 columns by convention.
type PayMerchantAccount struct {
	ent.Schema
}

// Mixin contributes the audit columns (ADR 0002).
func (PayMerchantAccount) Mixin() []ent.Mixin {
	return []ent.Mixin{AuditMixin{}}
}

// Fields of the PayMerchantAccount.
func (PayMerchantAccount) Fields() []ent.Field {
	return []ent.Field{
		// Snowflake ID; stored in the id column. Set by the repo on create
		// (see internal/infra/pay snowflake.go).
		field.Int64("id").Unique().Immutable(),
		// 1 WeChat Pay, 2 Alipay, 3 UnionPay (reserved), 4 Stripe (reserved).
		field.Int16("channel"),
		field.String("name").MaxLen(64),
		// 0 enabled, 1 disabled (RuoYi convention).
		field.Int16("status").Default(0),
		// Channel config JSON, encrypted whole (ADR 0018/0021). Format:
		// key_version:nonce_base64:ciphertext_base64.
		field.Text("config_cipher"),
		// Write-time masked echo of the config (plaintext, only masks) —
		// the list/detail display source, so display never decrypts.
		field.JSON("config_masked", map[string]string{}),
		// SHA-256 hex of the canonical plaintext config; the gateway cache
		// key's digest half — a config rewrite invalidates cached clients.
		field.String("config_digest").MaxLen(64),
		// Master-key version (KEK rotation reserve, ADR 0018).
		field.Int("key_version").Default(1),
		field.String("remark").MaxLen(500).Default(""),
	}
}

// Annotations pins the table name to pay_merchant_account (pay is not of
// RuoYi lineage — no sys_ prefix).
func (PayMerchantAccount) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "pay_merchant_account"},
	}
}

// Indexes enforces at most one active account per (channel, display name);
// withdrawn rows free the name for reuse (ADR 0002 soft delete).
func (PayMerchantAccount) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel", "name").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
