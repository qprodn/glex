// Package entity holds the pay module's domain objects: Merchant Accounts,
// Payments, Refunds, Channel Notifications, and the module's enumerations.
package entity

import (
	"strings"
	"time"
)

// Channel is one external payment provider the system collects money
// through, identified by its stable smallint code.
type Channel int16

const (
	ChannelWechat   Channel = 1 // 微信支付
	ChannelAlipay   Channel = 2 // 支付宝
	ChannelUnionPay Channel = 3 // 云闪付（预留）
	ChannelStripe   Channel = 4 // Stripe（预留）
)

// Valid reports whether the code is one of the known channels.
func (c Channel) Valid() bool { return c >= ChannelWechat && c <= ChannelStripe }

// Code is the stable lowercase identifier used in URLs (the notify route's
// channel path segment) and adapter registration.
func (c Channel) Code() string {
	switch c {
	case ChannelWechat:
		return "wechat"
	case ChannelAlipay:
		return "alipay"
	case ChannelUnionPay:
		return "unionpay"
	case ChannelStripe:
		return "stripe"
	}
	return ""
}

// Label is the display label.
func (c Channel) Label() string {
	switch c {
	case ChannelWechat:
		return "微信支付"
	case ChannelAlipay:
		return "支付宝"
	case ChannelUnionPay:
		return "云闪付"
	case ChannelStripe:
		return "Stripe"
	}
	return ""
}

// ChannelByCode resolves a stable code back to the Channel.
func ChannelByCode(code string) (Channel, bool) {
	switch strings.ToLower(code) {
	case "wechat":
		return ChannelWechat, true
	case "alipay":
		return ChannelAlipay, true
	case "unionpay":
		return ChannelUnionPay, true
	case "stripe":
		return ChannelStripe, true
	}
	return 0, false
}

// MerchantStatus is the Merchant Account switch: disabled accounts cannot
// back new Payments; in-flight ones still settle.
type MerchantStatus int16

const (
	MerchantEnabled  MerchantStatus = 0
	MerchantDisabled MerchantStatus = 1
)

// SchemaFieldType is the form control a schema field renders as.
type SchemaFieldType string

const (
	FieldText     SchemaFieldType = "text"
	FieldPassword SchemaFieldType = "password"
	FieldTextarea SchemaFieldType = "textarea"
	FieldSelect   SchemaFieldType = "select"
)

// SchemaOption is one labeled choice of a select field.
type SchemaOption struct {
	Value string
	Label string
}

// SchemaField is one field of a Channel's config schema — the unit the
// merchant form renders and writes validate against. Secret fields are
// encrypted at rest, masked on every read, and scrubbed from operation
// logs; on update, a blank secret means "keep the stored value" (ADR 0018,
// ADR 0021).
type SchemaField struct {
	Name        string
	Label       string
	Type        SchemaFieldType
	Required    bool
	Secret      bool
	Placeholder string
	Help        string
	Group       string
	Choices     []SchemaOption
	Default     string
}

// ChannelSchema is one Channel's config schema — defined once beside the
// channel's adapter and served verbatim to the merchant form (ADR 0021).
type ChannelSchema struct {
	Channel Channel
	Name    string
	Label   string
	Fields  []SchemaField
}

// MerchantAccount is the platform's own collecting account at one Channel.
// Config holds the decrypted plaintext and is populated only while a
// gateway is being resolved or the config is being rewritten — display
// paths read ConfigMasked and never decrypt (ADR 0018).
type MerchantAccount struct {
	ID           int64
	Channel      Channel
	Name         string
	Status       MerchantStatus
	Config       map[string]string // plaintext; nil unless decrypted
	ConfigCipher string            // AES-256-GCM token, never leaves the server
	ConfigMasked map[string]string // write-time masked echo (plaintext store)
	ConfigDigest string            // sha256 of the canonical plaintext config
	KeyVersion   int
	Remark       string
	CreateBy     int64
	UpdateBy     int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
