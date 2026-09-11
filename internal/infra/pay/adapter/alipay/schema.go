// Package alipay adapts Alipay's open platform to the pay module's channel
// seam. The config schema in this file is the single source the merchant
// form renders, writes validate against, and the masked echo derives from
// (ADR 0021); the gateway (gateway.go) hides the official SDK entirely.
// Both key mode and certificate mode are supported; certificate rotation
// is the SDK's concern.
package alipay

import (
	payentity "glex/internal/domain/pay/entity"
)

// ConfigSchema describes an Alipay application's credential set. The two
// modes share the app_id and the application private key and differ in how
// Alipay's side is trusted: alipay_public_key for key mode, the certificate
// trio (app public cert, Alipay public cert — the verification trust
// anchor — and root cert) for certificate mode (RequiredWhen drives both
// the form's conditionality and the server-side validation).
func ConfigSchema() payentity.ChannelSchema {
	return payentity.ChannelSchema{
		Channel: payentity.ChannelAlipay,
		Name:    "alipay",
		Label:   "支付宝",
		Fields: []payentity.SchemaField{
			{
				Name:     "app_id",
				Label:    "应用 ID（app_id）",
				Type:     payentity.FieldText,
				Required: true,
				Group:    "应用信息",
				Help:     "支付宝开放平台应用 APPID",
			},
			{
				Name:     "mode",
				Label:    "签名模式",
				Type:     payentity.FieldSelect,
				Required: true,
				Default:  "key",
				Group:    "应用信息",
				Choices: []payentity.SchemaOption{
					{Value: "key", Label: "公钥模式"},
					{Value: "cert", Label: "证书模式"},
				},
				Help: "公钥模式配支付宝公钥；证书模式配证书三元组",
			},
			{
				Name:        "app_private_key",
				Label:       "应用私钥",
				Type:        payentity.FieldTextarea,
				Required:    true,
				Secret:      true,
				Group:       "应用信息",
				Help:        "应用私钥（PKCS8），用于请求签名",
				Placeholder: "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQ...",
			},
			{
				Name:         "alipay_public_key",
				Label:        "支付宝公钥",
				Type:         payentity.FieldTextarea,
				Secret:       true,
				Group:        "应用信息",
				RequiredWhen: map[string]string{"mode": "key"},
				Help:         "公钥模式必填：用于验签的支付宝公钥",
			},
			{
				Name:         "app_public_cert",
				Label:        "应用公钥证书",
				Type:         payentity.FieldTextarea,
				Secret:       true,
				Group:        "证书模式",
				RequiredWhen: map[string]string{"mode": "cert"},
				Help:         "证书模式必填：应用公钥证书（appCertPublicKey.crt）全文",
			},
			{
				Name:         "alipay_public_cert",
				Label:        "支付宝公钥证书",
				Type:         payentity.FieldTextarea,
				Secret:       true,
				Group:        "证书模式",
				RequiredWhen: map[string]string{"mode": "cert"},
				Help:         "证书模式必填：支付宝公钥证书（alipayCertPublicKey_RSA2.crt）全文——验签的信任锚",
			},
			{
				Name:         "alipay_root_cert",
				Label:        "支付宝根证书",
				Type:         payentity.FieldTextarea,
				Secret:       true,
				Group:        "证书模式",
				RequiredWhen: map[string]string{"mode": "cert"},
				Help:         "证书模式必填：支付宝根证书（alipayRootCert.crt）全文",
			},
		},
	}
}
