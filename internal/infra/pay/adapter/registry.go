// Package adapter hosts the channel adapter seam's shared machinery: the
// Factory every channel package exposes and the Registry that resolves
// Merchant Accounts into cached gateways and serves the config schemas the
// merchant forms are built from (ADR 0021 — adding a channel is one
// adapter package plus one schema, never a core change).
package adapter

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	pay "glex/internal/domain/pay"
	payentity "glex/internal/domain/pay/entity"
	payservice "glex/internal/domain/pay/service"
)

// GatewayOption customises one gateway build — the seam the contract
// tests use to point an adapter at a fake channel server.
type GatewayOption func(*GatewayOptions)

// GatewayOptions carries the resolved build options.
type GatewayOptions struct {
	// BaseURL overrides the channel's API endpoint (tests only).
	BaseURL string
	// Timeout bounds every outbound call; zero uses the adapter's
	// channel-side default.
	Timeout time.Duration
}

// WithBaseURL points the gateway at a custom API endpoint.
func WithBaseURL(url string) GatewayOption {
	return func(o *GatewayOptions) { o.BaseURL = url }
}

// WithTimeout bounds the gateway's outbound calls.
func WithTimeout(d time.Duration) GatewayOption {
	return func(o *GatewayOptions) { o.Timeout = d }
}

// Factory is what one channel package exposes: its config schema and a
// gateway builder for a validated config value set.
type Factory interface {
	Schema() payentity.ChannelSchema
	NewGateway(cfg map[string]string, opts ...GatewayOption) (payservice.ChannelGateway, error)
}

// Registry implements the domain's ChannelCatalog and GatewayRegistry
// ports over the registered factories. Gateway builds are expensive
// (WeChat pulls platform certificates), so built gateways are cached per
// (merchant, config digest) — a config rewrite, a disable, or a
// withdrawal takes effect on the next use without a restart.
type Registry struct {
	mu        sync.RWMutex
	factories map[payentity.Channel]Factory
	gateways  map[string]payservice.ChannelGateway
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[payentity.Channel]Factory),
		gateways:  make(map[string]payservice.ChannelGateway),
	}
}

// Register adds a channel factory (last one wins for a channel code).
func (r *Registry) Register(f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[f.Schema().Channel] = f
}

// ---- ChannelCatalog ----

// Schemas lists every registered channel's schema, ordered by channel
// code for a stable API response.
func (r *Registry) Schemas() []payentity.ChannelSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]payentity.ChannelSchema, 0, len(r.factories))
	for _, f := range r.factories {
		schemas = append(schemas, f.Schema())
	}
	sort.Slice(schemas, func(i, j int) bool { return schemas[i].Channel < schemas[j].Channel })
	return schemas
}

// Schema returns one channel's schema; the module's unadopted sentinel for
// channels with no registered adapter (ADR 0025 — the sentinel over the
// zero value, never a bool flag).
func (r *Registry) Schema(channel payentity.Channel) (payentity.ChannelSchema, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[channel]
	if !ok {
		return payentity.ChannelSchema{}, pay.ErrChannelNotAdopted
	}
	return f.Schema(), nil
}

// Adopted reports whether the channel has a registered adapter.
func (r *Registry) Adopted(channel payentity.Channel) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[channel]
	return ok
}

// ValidateConfig checks values against the channel's schema — unknown
// keys rejected, required (or mode-required) fields present, select
// values in range — applies defaults, and returns the cleaned full set:
// every schema field present, whitespace-trimmed except multiline fields.
func (r *Registry) ValidateConfig(channel payentity.Channel, values map[string]string) (map[string]string, error) {
	r.mu.RLock()
	f, ok := r.factories[channel]
	r.mu.RUnlock()
	if !ok {
		return nil, pay.ErrChannelNotAdopted
	}
	schema := f.Schema()
	byName := make(map[string]payentity.SchemaField, len(schema.Fields))
	for _, field := range schema.Fields {
		byName[field.Name] = field
	}
	for name := range values {
		if _, known := byName[name]; !known {
			return nil, fmt.Errorf("%w: unknown config field %q", pay.ErrMerchantInvalid, name)
		}
	}
	cleaned := make(map[string]string, len(schema.Fields))
	for _, field := range schema.Fields {
		v := strings.TrimSpace(values[field.Name])
		if field.Type == payentity.FieldTextarea {
			// Multiline material (keys, certificates) is never trimmed.
			v = values[field.Name]
		}
		if v == "" && field.Default != "" {
			v = field.Default
		}
		if v == "" && fieldRequiredBy(field, cleaned) {
			return nil, fmt.Errorf("%w: %q is required", pay.ErrMerchantInvalid, field.Label)
		}
		if field.Type == payentity.FieldSelect && v != "" {
			match := false
			for _, choice := range field.Choices {
				if choice.Value == v {
					match = true
					break
				}
			}
			if !match {
				return nil, fmt.Errorf("%w: %q must be one of the listed options", pay.ErrMerchantInvalid, field.Label)
			}
		}
		cleaned[field.Name] = v
	}
	return cleaned, nil
}

// MaskConfig renders the write-time masked echo of a full value set: a
// secret shows only its last four characters (or a bare **** when too
// short), everything else reads verbatim. Fields with no value are
// omitted — "what is set" is exactly what the echo shows (ADR 0018).
func (r *Registry) MaskConfig(channel payentity.Channel, values map[string]string) map[string]string {
	r.mu.RLock()
	f, ok := r.factories[channel]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	masked := make(map[string]string)
	for _, field := range f.Schema().Fields {
		v := values[field.Name]
		if v == "" {
			continue
		}
		if field.Secret {
			masked[field.Name] = maskSecret(v)
		} else {
			masked[field.Name] = v
		}
	}
	return masked
}

// ---- GatewayRegistry ----

// Resolve returns the merchant's cached gateway, building one on a cache
// miss. Merchant.Config must hold the decrypted values (the caller ran
// LoadConfig). Unadopted channels surface as the module's sentinel.
func (r *Registry) Resolve(ctx context.Context, m *payentity.MerchantAccount) (payservice.ChannelGateway, error) {
	if m.Config == nil {
		return nil, pay.ErrConfigCrypto
	}
	r.mu.RLock()
	f, ok := r.factories[m.Channel]
	key := gatewayKey(m.ID, m.ConfigDigest)
	cached := r.gateways[key]
	r.mu.RUnlock()
	if !ok {
		return nil, pay.ErrChannelNotAdopted
	}
	if cached != nil {
		return cached, nil
	}
	gw, err := f.NewGateway(m.Config)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.gateways[key] = gw
	r.mu.Unlock()
	return gw, nil
}

// Invalidate drops every cached gateway of the merchant.
func (r *Registry) Invalidate(merchantID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := fmt.Sprintf("%d:", merchantID)
	for key := range r.gateways {
		if strings.HasPrefix(key, prefix) {
			delete(r.gateways, key)
		}
	}
}

func gatewayKey(merchantID int64, digest string) string {
	return fmt.Sprintf("%d:%s", merchantID, digest)
}

// maskSecret keeps the tail visible and hides the rest: `****ab12` for
// values long enough, a bare **** otherwise (ADR 0018's example shape).
func maskSecret(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

// fieldRequiredBy evaluates a field's requirement against the values
// already cleaned: statically required, or mode-required with every named
// sibling currently holding the listed value.
func fieldRequiredBy(f payentity.SchemaField, cleaned map[string]string) bool {
	if f.Required {
		return true
	}
	if len(f.RequiredWhen) == 0 {
		return false
	}
	for name, want := range f.RequiredWhen {
		if cleaned[name] != want {
			return false
		}
	}
	return true
}
