package adapter

import (
	"context"
	"errors"
	"net/http"
	"testing"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
)

// The registry tests use only fake factories — the real wechat/alipay
// schemas are exercised in payinfra's adapter tests (this package's
// factories import it, so they cannot be imported here).

func newFakeRegistry() *Registry {
	r := NewRegistry()
	r.Register(&fakeFactory{channel: payentity.ChannelWechat})
	return r
}

func fakeValues() map[string]string {
	return map[string]string{"mchid": "1900000001", "api_v3_key": "k"}
}

func TestValidateRejectsUnadoptedChannel(t *testing.T) {
	r := newFakeRegistry()
	if _, err := r.ValidateConfig(payentity.ChannelStripe, map[string]string{}); !errors.Is(err, pay.ErrChannelNotAdopted) {
		t.Fatalf("err = %v, want ErrChannelNotAdopted", err)
	}
	if r.Adopted(payentity.ChannelStripe) {
		t.Fatal("unregistered channel reported adopted")
	}
	if _, ok := r.Schema(payentity.ChannelStripe); ok {
		t.Fatal("unregistered channel returned a schema")
	}
}

func TestResolveCachesPerConfigDigest(t *testing.T) {
	r := newFakeRegistry()
	m := &payentity.MerchantAccount{
		ID:           42,
		Channel:      payentity.ChannelWechat,
		Config:       fakeValues(),
		ConfigDigest: "digest-1",
	}
	gw1, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	gw2, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if gw1 != gw2 {
		t.Fatal("resolve did not return the cached gateway")
	}
	// A config rewrite (new digest) must build a fresh gateway.
	m.ConfigDigest = "digest-2"
	gw3, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("resolve after rewrite: %v", err)
	}
	if gw3 == gw1 {
		t.Fatal("stale gateway served after config rewrite")
	}
	// Withdrawal/disable drops the cache.
	r.Invalidate(42)
	gw4, err := r.Resolve(context.Background(), m)
	if err != nil {
		t.Fatalf("resolve after invalidate: %v", err)
	}
	if gw4 == gw3 {
		t.Fatal("gateway cache survived Invalidate")
	}
}

func TestResolveRequiresDecryptedConfig(t *testing.T) {
	r := newFakeRegistry()
	m := &payentity.MerchantAccount{ID: 1, Channel: payentity.ChannelWechat}
	if _, err := r.Resolve(context.Background(), m); !errors.Is(err, pay.ErrConfigCrypto) {
		t.Fatalf("err = %v, want ErrConfigCrypto for nil config", err)
	}
}

// ---- fake gateway + factory ----

type fakeGateway struct {
	channel payentity.Channel
}

func (f *fakeGateway) Channel() payentity.Channel { return f.channel }

func (f *fakeGateway) Prepay(ctx context.Context, req payservice.PrepayRequest) (payservice.PrepayResult, error) {
	return payservice.PrepayResult{}, nil
}

func (f *fakeGateway) Query(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) (payservice.ChannelPaymentState, error) {
	return payservice.ChannelPaymentState{}, nil
}

func (f *fakeGateway) Close(ctx context.Context, m *payentity.MerchantAccount, paymentNo string) error {
	return nil
}

func (f *fakeGateway) Refund(ctx context.Context, req payservice.RefundRequest) (payservice.ChannelRefundState, error) {
	return payservice.ChannelRefundState{}, nil
}

func (f *fakeGateway) QueryRefund(ctx context.Context, m *payentity.MerchantAccount, refundNo, paymentNo string) (payservice.ChannelRefundState, error) {
	return payservice.ChannelRefundState{}, nil
}

func (f *fakeGateway) ParseNotification(ctx context.Context, m *payentity.MerchantAccount, headers http.Header, body []byte) (payentity.ChannelNotification, error) {
	return payentity.ChannelNotification{}, nil
}

func (f *fakeGateway) Probe(ctx context.Context, m *payentity.MerchantAccount) error { return nil }

type fakeFactory struct{ channel payentity.Channel }

func (f *fakeFactory) Schema() payentity.ChannelSchema {
	return payentity.ChannelSchema{
		Channel: f.channel,
		Name:    "fake",
		Label:   "Fake",
		Fields: []payentity.SchemaField{
			{Name: "mchid", Required: true},
			{Name: "api_v3_key", Secret: true, Required: true},
		},
	}
}

func (f *fakeFactory) NewGateway(cfg map[string]string, opts ...GatewayOption) (payservice.ChannelGateway, error) {
	return &fakeGateway{channel: f.channel}, nil
}
