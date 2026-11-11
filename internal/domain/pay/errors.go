// Package pay owns the pay module per ADR 0005: the Channel Merchant
// Accounts the back office configures and the Payments and Refunds
// collected through them. The package root holds only the typed errors;
// the objects live in entity, the row ports in repository, and the module's
// own rules — plus the channel gateway seam — in service.
package pay

import (
	"github.com/go-kratos/kratos/v3/errors"
)

var (
	// ErrMerchantNotFound: the requested Merchant Account does not exist
	// (or is withdrawn). Also the notify route's answer for an unknown
	// merchant path.
	ErrMerchantNotFound = errors.NotFound("MERCHANT_NOT_FOUND", "merchant account not found")
	// ErrMerchantInvalid: a Merchant Account parameter is invalid — blank
	// name, unknown schema field, missing required value, over-length
	// remark.
	ErrMerchantInvalid = errors.BadRequest("MERCHANT_INVALID", "merchant account parameters are invalid")
	// ErrMerchantDuplicate: the display name is already taken by an active
	// account of the same channel.
	ErrMerchantDuplicate = errors.Conflict("MERCHANT_DUPLICATE", "merchant account name already exists")
	// ErrChannelNotAdopted: the Channel has no registered adapter —
	// configuration of an unadopted channel, or a probe/payment on one.
	ErrChannelNotAdopted = errors.BadRequest("CHANNEL_NOT_ADOPTED", "channel is not adopted")
	// ErrMasterKeyMissing: PAY_MASTER_KEY is absent or malformed (ADR 0018)
	// — merchant configuration refuses to operate rather than degrade to
	// plaintext.
	ErrMasterKeyMissing = errors.InternalServer("MASTER_KEY_MISSING", "payment master key is not configured")
	// ErrConfigCrypto: the stored ciphertext failed to decrypt or
	// authenticate — corrupted rows or a wrong master key.
	ErrConfigCrypto = errors.InternalServer("CONFIG_CRYPTO_ERROR", "merchant config decryption failed")
	// ErrPaymentNotFound: the requested Payment does not exist (or is
	// soft-deleted).
	ErrPaymentNotFound = errors.NotFound("PAYMENT_NOT_FOUND", "payment not found")
	// ErrPaymentInvalid: a Payment parameter is invalid — non-positive
	// amount, unsupported currency (MVP: CNY), unsupported pay method on
	// a new Payment, blank biz_no or subject, or a missing Pay Application id.
	ErrPaymentInvalid = errors.BadRequest("PAYMENT_INVALID", "payment parameters are invalid")
	// ErrPaymentConflict: this Pay Application already has a Payment for
	// the Business Order Number, and the amount or Pay Method differs.
	ErrPaymentConflict = errors.Conflict("PAYMENT_CONFLICT", "payment amount or pay method differs from the existing payment")
	// ErrPaymentNotPaid: the Payment is not in the state the operation
	// needs — refunding an unpaid Payment.
	ErrPaymentNotPaid = errors.Conflict("PAYMENT_NOT_PAID", "payment is not paid")
	// ErrRefundNotFound: the requested Refund does not exist (or is
	// soft-deleted).
	ErrRefundNotFound = errors.NotFound("REFUND_NOT_FOUND", "refund not found")
	// ErrRefundExceedLimit: refunded + requested would exceed the paid
	// amount.
	ErrRefundExceedLimit = errors.Conflict("REFUND_EXCEED_LIMIT", "refund exceeds the paid amount")
	// ErrChannelError: the Channel rejected or failed the
	// server-to-server call — prepay, query, close, refund, probe.
	ErrChannelError = errors.New(502, "CHANNEL_ERROR", "payment channel request failed")
	// ErrNotifyInvalid: a Channel notification failed signature
	// verification and was recorded unverified, then rejected.
	ErrNotifyInvalid = errors.BadRequest("CHANNEL_NOTIFY_INVALID", "channel notification failed verification")
	// ErrPayStoreUnavailable: a pay-store operation failed unexpectedly
	// (connectivity, deadlock) — the raw error is logged at the handling
	// site and this sentinel crosses the boundary instead (ADR 0007).
	ErrPayStoreUnavailable = errors.ServiceUnavailable("PAY_STORE_UNAVAILABLE", "payment store unavailable")
	// ErrApplicationNotFound: the requested Pay Application does not exist
	// (or is withdrawn).
	ErrApplicationNotFound = errors.NotFound("APPLICATION_NOT_FOUND", "pay application not found")
	// ErrApplicationInvalid: a Pay Application parameter is invalid — blank
	// or over-length display name, a result address that is not an absolute
	// public http(s) URL, or a status other than enabled or disabled.
	ErrApplicationInvalid = errors.BadRequest("APPLICATION_INVALID", "pay application parameters are invalid")
	// ErrApplicationDuplicate: the display name is already taken by a Pay
	// Application that is not withdrawn.
	ErrApplicationDuplicate = errors.Conflict("APPLICATION_DUPLICATE", "pay application name already exists")
	// ErrBusinessNoticeNotFound: the requested Business Notification does
	// not exist (or is withdrawn).
	ErrBusinessNoticeNotFound = errors.NotFound("BUSINESS_NOTICE_NOT_FOUND", "business notice not found")
	// ErrBusinessNoticeNotExhausted: manual send was asked for a notice
	// that is still waiting or already accepted.
	ErrBusinessNoticeNotExhausted = errors.Conflict("BUSINESS_NOTICE_NOT_EXHAUSTED", "business notice is not exhausted")
	// ErrBusinessNoticeAddressEmpty: the Pay Application's current result
	// address of the matching kind is empty, so there is nowhere to send.
	ErrBusinessNoticeAddressEmpty = errors.BadRequest("BUSINESS_NOTICE_ADDRESS_EMPTY", "pay application result address is empty")
	// ErrBusinessNoticeInconsistent: a stored notice has a kind other than
	// payment or refund, or its body cannot be built.
	ErrBusinessNoticeInconsistent = errors.InternalServer("BUSINESS_NOTICE_INCONSISTENT", "business notice is inconsistent")
)
