// Package wechat adapts WeChat Pay APIv3 to the pay module's channel seam.
// The config schema in this file is the single source the merchant form
// renders, writes validate against, and the masked echo derives from
// (ADR 0021); the gateway (gateway.go) hides the official SDK entirely.
package wechat

import (
	payentity "glex/internal/domain/pay/entity"
)

// ConfigSchema describes a WeChat Pay merchant's APIv3 credential set.
// Every field is secret or public identity material; the schema marks
// which values never appear unmasked (ADR 0018).
func ConfigSchema() payentity.ChannelSchema {
	return payentity.ChannelSchema{
		Channel: payentity.ChannelWechat,
		Name:    "wechat",
		Label:   "微信支付",
		Fields: []payentity.SchemaField{
			{
				Name:     "mchid",
				Label:    "商户号",
				Type:     payentity.FieldText,
				Required: true,
				Group:    "商户信息",
				Help:     "微信支付商户号（mchId）",
			},
			{
				Name:        "appid",
				Label:       "应用 ID（appid）",
				Type:        payentity.FieldText,
				Required:    true,
				Group:       "商户信息",
				Help:        "Native 支付建议绑定公众号/开放平台 appid；JSAPI 支付必填（预留）",
				Placeholder: "wx1234567890abcdef",
			},
			{
				Name:     "api_v3_key",
				Label:    "APIv3 密钥",
				Type:     payentity.FieldPassword,
				Required: true,
				Secret:   true,
				Group:    "商户信息",
				Help:     "商户平台设置的 APIv3 密钥（32 位），用于回调报文解密",
			},
			{
				Name:     "merchant_serial",
				Label:    "商户证书序列号",
				Type:     payentity.FieldText,
				Required: true,
				Group:    "证书与密钥",
				Help:     "商户 API 证书的序列号",
			},
			{
				Name:        "merchant_private_key",
				Label:       "商户私钥（PEM）",
				Type:        payentity.FieldTextarea,
				Required:    true,
				Secret:      true,
				Group:       "证书与密钥",
				Help:        "商户 apiclient_key.pem 的完整内容，用于请求签名",
				Placeholder: "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----",
			},
		},
	}
}
