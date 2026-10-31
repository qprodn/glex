// Package service holds the pay module's domain services and the channel
// gateway seam. The gateway port is deliberately declared beside the domain
// rules it feeds and implemented in infra's adapter packages — adding a
// channel is one adapter package plus one config schema, never a change to
// the tables, the API, or the state machines (design §1).
package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"glex/internal/domain/pay/entity"
)

// PrepayRequest is one unified prepay call. Amount is int64 minor units
// (ADR 0019); unit conventions are absorbed inside the adapter.
type PrepayRequest struct {
	// Merchant carries the decrypted config (Config must be populated).
	Merchant  *entity.MerchantAccount
	PaymentNo string // the Payment number — the Channel's out_trade_no
	Amount    int64
	Currency  string
	Subject   string
	ExpireAt  time.Time
	Method    entity.PayMethod // MVP: Native
	// NotifyURL is the absolute callback URL the Channel pushes to —
	// /pay/notify/{channel}/{merchant_id} on this deployment.
	NotifyURL string
}

// PrepayResult carries the method-specific payload the front end renders —
// Native: the code_url a QR encodes.
type PrepayResult struct {
	ChannelPayload string
}

// QueryRequest is one unified payment-state query call.
type QueryRequest struct {
	// Merchant carries the decrypted config (Config must be populated).
	Merchant  *entity.MerchantAccount
	PaymentNo string // the Payment number — the Channel's out_trade_no
}

// CloseRequest is one unified channel-side close call.
type CloseRequest struct {
	// Merchant carries the decrypted config (Config must be populated).
	Merchant  *entity.MerchantAccount
	PaymentNo string // the Payment number — the Channel's out_trade_no
}

// RefundQueryRequest is one unified refund-state query call. Alipay's
// refund query requires the owning payment's out_trade_no alongside the
// out_refund_no; channels that don't need it ignore the extra field.
type RefundQueryRequest struct {
	// Merchant carries the decrypted config (Config must be populated).
	Merchant  *entity.MerchantAccount
	RefundNo  string // the Refund number — the Channel's out_refund_no
	PaymentNo string // the Payment number — the Channel's out_trade_no
}

// NotificationRequest is one channel delivery: the merchant whose gateway
// verifies it plus the raw HTTP evidence.
type NotificationRequest struct {
	// Merchant carries the decrypted config (Config must be populated).
	Merchant *entity.MerchantAccount
	Headers  http.Header
	Body     []byte
}

// ChannelPaymentState is the Channel's asserted Payment state, already
// normalized to the module's own status vocabulary.
type ChannelPaymentState struct {
	State          entity.PaymentStatus // Pending / Paid / Failed / Closed
	ChannelTradeNo string
	PayerID        string
	PaidAt         time.Time
	// Amount is the channel-asserted paid total in minor units (ADR 0019).
	// The advance path must verify it against the Payment row before the
	// CAS — a signature-valid success naming a different amount never
	// settles. Zero means "not asserted" and skips the check.
	Amount     int64
	FailReason string
}

// RefundRequest is one unified refund call. Total is the original payment's
// full amount for channels whose refund API wants it; Amount is this
// refund's size. Minor units throughout (ADR 0019).
type RefundRequest struct {
	// Merchant carries the decrypted config (Config must be populated).
	Merchant       *entity.MerchantAccount
	RefundNo       string // the Refund number — the Channel's out_refund_no
	PaymentNo      string // the Payment number — the Channel's out_trade_no
	ChannelTradeNo string // the Channel's trade number, when known
	Total          int64
	Amount         int64
	Reason         string
	// NotifyURL is the absolute callback URL refund outcomes push to —
	// /pay/notify/{channel}/{merchant_id} on this deployment.
	NotifyURL string
}

// ChannelRefundState is the Channel's asserted Refund state.
type ChannelRefundState struct {
	State           entity.RefundStatus // Pending / Success / Failed
	ChannelRefundNo string
	RefundedAt      time.Time
	// RefundAmount is the channel-asserted refunded amount in minor units
	// (ADR 0019), verified against the Refund row before the settle CAS.
	// Zero means "not asserted" and skips the check.
	RefundAmount int64
	FailReason   string
}

// ChannelGateway is the per-channel seam the whole module talks through.
// Implementations live in internal/infra/pay/adapter/<channel> and hide
// their SDK entirely; every outbound call runs under a channel-side timeout.
// Verification failures from ParseNotification are errors — the caller
// rejects the delivery (ADR 0020).
type ChannelGateway interface {
	Channel() entity.Channel
	// Prepay creates the channel-side order for one Payment.
	Prepay(ctx context.Context, req PrepayRequest) (PrepayResult, error)
	// Query asks the Channel for one Payment's true state.
	Query(ctx context.Context, req QueryRequest) (ChannelPaymentState, error)
	// Close closes one Payment at the Channel. A "already paid" answer is
	// ErrChannelAlreadyPaid — an error the caller flips into a query.
	Close(ctx context.Context, req CloseRequest) error
	// Refund requests one refund; synchronous channels return the settled
	// state, asynchronous ones Pending.
	Refund(ctx context.Context, req RefundRequest) (ChannelRefundState, error)
	// QueryRefund asks the Channel for one Refund's true state.
	QueryRefund(ctx context.Context, req RefundQueryRequest) (ChannelRefundState, error)
	// ParseNotification verifies the delivery's signature, decrypts and
	// parses its payload into the canonical event. A verification or
	// decryption failure returns an error — the caller records the
	// unverified evidence and rejects.
	ParseNotification(ctx context.Context, req NotificationRequest) (entity.ChannelNotification, error)
	// Probe runs the Connectivity Test: one cheap authenticated call that
	// moves no money and leaves no Payment.
	Probe(ctx context.Context, m *entity.MerchantAccount) error
	// AckSuccess is the channel's success response body. The intake writes
	// it only after it has accepted the delivery (ADR 0020, ADR 0027).
	AckSuccess() string
}

// ErrChannelAlreadyPaid is returned by Close when the Channel reports the
// payment already paid — the settlement path flips it into a query and
// advances the Payment to PAID. It never crosses the wire directly.
var ErrChannelAlreadyPaid = errors.New("pay: channel reports the payment already paid")

// GatewayRegistry resolves a Merchant Account's decrypted config into a
// cached ChannelGateway. Domain port; infra implementation. The cache key
// is (merchant, config digest), so a config rewrite takes effect without a
// restart, and disabling or withdrawing a merchant drops its entries.
type GatewayRegistry interface {
	// Resolve returns the merchant's gateway; Merchant.Config must be
	// populated (the caller decrypted it).
	Resolve(ctx context.Context, m *entity.MerchantAccount) (ChannelGateway, error)
	// Invalidate drops the merchant's cached gateways.
	Invalidate(merchantID int64)
}

// ChannelConfigRequest is one schema-driven config pass: the channel whose
// schema governs the values.
type ChannelConfigRequest struct {
	Channel entity.Channel
	Values  map[string]string
}

// ChannelCatalog exposes the channel config schemas and the schema-driven
// derivations the merchant use cases are built from (ADR 0021 — the schema
// is defined beside its adapter and served through this port).
type ChannelCatalog interface {
	// Schemas lists every registered channel's schema.
	Schemas() []entity.ChannelSchema
	// Schema returns one channel's schema; a channel with no registered
	// adapter fails with the module's unadopted sentinel (ADR 0025 —
	// missing is the sentinel over the zero value, never a bool flag).
	Schema(channel entity.Channel) (entity.ChannelSchema, error)
	// Adopted reports whether the channel has a registered adapter and can
	// collect.
	Adopted(channel entity.Channel) bool
	// ValidateConfig checks values against the channel's schema (unknown
	// keys rejected, required fields present, select values in range) and
	// applies defaults; it returns the cleaned full value set.
	ValidateConfig(req ChannelConfigRequest) (map[string]string, error)
	// MaskConfig renders the write-time masked echo of a full value set.
	MaskConfig(req ChannelConfigRequest) map[string]string
}

// ConfigCipher is the credential-envelope port (ADR 0018): the whole
// config JSON encrypted under the env-held master key, plus the canonical
// digest the gateway cache keys on. Absent or malformed PAY_MASTER_KEY
// surfaces as the module's master-key sentinel on every call.
type ConfigCipher interface {
	// Encrypt seals the value set into the stored token
	// (key_version:nonce_base64:ciphertext_base64).
	Encrypt(values map[string]string) (string, error)
	// Decrypt opens a stored token back into the value set.
	Decrypt(token string) (map[string]string, error)
	// Digest is the sha256 hex of the canonical plaintext config.
	Digest(values map[string]string) string
}
