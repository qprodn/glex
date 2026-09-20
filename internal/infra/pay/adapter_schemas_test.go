package payinfra

import (
	"context"
	"errors"
	"testing"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
	"glex/internal/infra/pay/adapter"
	"glex/internal/infra/pay/adapter/alipay"
	"glex/internal/infra/pay/adapter/wechat"
	"glex/internal/infra/pay/crypt"
)

// The adapter schema tests live at this level because the registry
// (adapter package) and the channel schema packages form a cycle from the
// test files' perspective: payinfra sees both sides.

func newAdoptedRegistry() *adapter.Registry {
	r := adapter.NewRegistry()
	r.Register(wechat.NewFactory())
	r.Register(alipay.NewFactory())
	return r
}

func wechatValues() map[string]string {
	return map[string]string{
		"mchid":                "1900000001",
		"appid":                "wx1234567890abcdef",
		"api_v3_key":           "a1b2c3d4e5f6g7h8a1b2c3d4e5f6g7h8",
		"merchant_serial":      "5157F09EFDC096DE15EBE81A47057A72",
		"merchant_private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvQ\n-----END PRIVATE KEY-----",
	}
}

func alipayValues(mode string) map[string]string {
	v := map[string]string{
		"app_id":          "2021000100000001",
		"mode":            mode,
		"app_private_key": "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQ",
	}
	if mode == "key" {
		v["alipay_public_key"] = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA"
	} else {
		v["app_public_cert"] = "-----BEGIN CERTIFICATE-----\napp\n-----END CERTIFICATE-----"
		v["alipay_public_cert"] = "-----BEGIN CERTIFICATE-----\nalipay\n-----END CERTIFICATE-----"
		v["alipay_root_cert"] = "-----BEGIN CERTIFICATE-----\nroot\n-----END CERTIFICATE-----"
	}
	return v
}

func TestValidateWechatConfig(t *testing.T) {
	r := newAdoptedRegistry()
	cleaned, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelWechat, Values: wechatValues()})
	if err != nil {
		t.Fatalf("valid wechat config rejected: %v", err)
	}
	if len(cleaned) != 5 {
		t.Fatalf("cleaned has %d fields, want the schema's 5", len(cleaned))
	}
}

func TestValidateRejectsUnknownKeyAndMissingRequired(t *testing.T) {
	r := newAdoptedRegistry()
	values := wechatValues()
	values["nope"] = "x"
	if _, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelWechat, Values: values}); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("unknown key: err = %v, want ErrMerchantInvalid", err)
	}
	values = wechatValues()
	delete(values, "api_v3_key")
	if _, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelWechat, Values: values}); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("missing required: err = %v, want ErrMerchantInvalid", err)
	}
}

func TestValidateSelectAndDefaults(t *testing.T) {
	r := newAdoptedRegistry()
	values := alipayValues("key")
	delete(values, "mode")
	cleaned, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelAlipay, Values: values})
	if err != nil {
		t.Fatalf("default mode validation failed: %v", err)
	}
	if cleaned["mode"] != "key" {
		t.Fatalf("mode = %q, want default key", cleaned["mode"])
	}
	bad := alipayValues("card")
	if _, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelAlipay, Values: bad}); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("err = %v, want ErrMerchantInvalid", err)
	}
}

func TestValidateAlipayModeRequirements(t *testing.T) {
	r := newAdoptedRegistry()
	if _, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelAlipay, Values: alipayValues("key")}); err != nil {
		t.Fatalf("key mode rejected: %v", err)
	}
	if _, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelAlipay, Values: alipayValues("cert")}); err != nil {
		t.Fatalf("cert mode rejected: %v", err)
	}
	missing := alipayValues("cert")
	delete(missing, "alipay_root_cert")
	if _, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelAlipay, Values: missing}); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("cert trio incomplete: err = %v, want ErrMerchantInvalid", err)
	}
	missing = alipayValues("key")
	delete(missing, "alipay_public_key")
	if _, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelAlipay, Values: missing}); !errors.Is(err, pay.ErrMerchantInvalid) {
		t.Fatalf("key mode missing public key: err = %v, want ErrMerchantInvalid", err)
	}
}

func TestValidateTrimsButKeepsMultiline(t *testing.T) {
	r := newAdoptedRegistry()
	values := wechatValues()
	values["mchid"] = " 1900000001 \n"
	cleaned, err := r.ValidateConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelWechat, Values: values})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cleaned["mchid"] != "1900000001" {
		t.Fatalf("mchid = %q, want trimmed", cleaned["mchid"])
	}
	if cleaned["merchant_private_key"] != values["merchant_private_key"] {
		t.Fatal("multiline private key was altered")
	}
}

func TestMaskConfigEchoesWhatIsSet(t *testing.T) {
	r := newAdoptedRegistry()
	values := wechatValues()
	masked := r.MaskConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelWechat, Values: values})
	// Secrets show only their tail (ADR 0018's `****ab12` shape).
	if got := masked["api_v3_key"]; got != "****g7h8" {
		t.Fatalf("masked api_v3_key = %q", got)
	}
	if got := masked["merchant_private_key"]; got != "****----" {
		t.Fatalf("masked merchant_private_key = %q", got)
	}
	// Plain identity material reads verbatim.
	if got := masked["mchid"]; got != values["mchid"] {
		t.Fatalf("masked mchid = %q, want verbatim", got)
	}
}

func TestMaskShortSecretIsBareAndEmptyFieldsAbsent(t *testing.T) {
	r := newAdoptedRegistry()
	values := alipayValues("key")
	values["app_private_key"] = "abc"
	delete(values, "alipay_public_key") // unset → must be absent from the echo
	masked := r.MaskConfig(payservice.ChannelConfigRequest{Channel: payentity.ChannelAlipay, Values: values})
	if masked["app_private_key"] != "****" {
		t.Fatalf("masked short secret = %q, want bare ****", masked["app_private_key"])
	}
	if _, ok := masked["alipay_public_key"]; ok {
		t.Fatal("empty field leaked into the masked echo")
	}
}

func TestAdoptedSchemaListing(t *testing.T) {
	r := newAdoptedRegistry()
	schemas := r.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("schemas = %d, want 2", len(schemas))
	}
	if schemas[0].Channel >= schemas[1].Channel {
		t.Fatalf("schemas not ordered by channel: %d, %d", schemas[0].Channel, schemas[1].Channel)
	}
	for _, s := range schemas {
		if s.Name == "" || s.Label == "" || len(s.Fields) == 0 {
			t.Fatalf("channel %d schema incomplete", s.Channel)
		}
	}
}

// The stub gateways report not-implemented until their tickets land —
// schema-first, as the design orders it.
func TestStubGatewaysNotYetResolved(t *testing.T) {
	r := newAdoptedRegistry()
	m := &payentity.MerchantAccount{
		ID:           1,
		Channel:      payentity.ChannelWechat,
		Config:       wechatValues(),
		ConfigDigest: "d",
	}
	if _, err := r.Resolve(context.Background(), m); err == nil || errors.Is(err, pay.ErrChannelNotAdopted) {
		t.Fatalf("stub gateway resolved or misreported: %v", err)
	}
	_ = crypt.EnvMasterKey
}
