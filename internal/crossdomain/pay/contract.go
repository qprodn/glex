// Package pay publishes the pay module's cross-module contracts in the
// anti-corruption shape of ADR 0005: contract.go holds the consumer-shaped
// interfaces, model/ the contract DTOs, impl/ the adapters (the creator
// shares the back office's application use case — locks and transaction
// boundaries live there — and the reader adapts the payment domain
// service), and paymock/ holds the mockgen-generated test double. Errors
// cross the seam as the pay module's kratos errors — the wire shape is
// the contract's.
package pay

import (
	"context"

	"glex/internal/crossdomain/pay/model"
)

//go:generate go tool mockgen -package paymock -destination paymock/payment_creator.go glex/internal/crossdomain/pay PaymentCreator
//go:generate go tool mockgen -package paymock -destination paymock/payment_reader.go glex/internal/crossdomain/pay PaymentReader

// PaymentCreator initiates payments for business modules calling in
// process (design §8): the same initiation the back office API uses,
// including the duplicate-submit lock.
type PaymentCreator interface {
	// CreatePayment initiates one Native payment and returns it carrying
	// the QR payload. A concurrent duplicate submit for the same
	// (merchant, business order number) is rejected with LOCK_BUSY.
	CreatePayment(ctx context.Context, req *model.PaymentRequest) (*model.Payment, error)
}

// PaymentReader reads payment outcomes on demand (design §8) — the pull
// counterpart of the notify route. Notify forwarding to business
// callbacks is a later evolution (ADR 0020's design session).
type PaymentReader interface {
	// GetPayment resolves one payment by its number, settling it first
	// when PENDING and expired.
	GetPayment(ctx context.Context, paymentNo string) (*model.Payment, error)
	// GetByBizNo resolves a merchant's newest payment for a Business
	// Order Number, with the same settlement-on-read.
	GetByBizNo(ctx context.Context, merchantID int64, bizNo string) (*model.Payment, error)
}
