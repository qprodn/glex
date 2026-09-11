package entity

import (
	"time"

	"glex/internal/domain/pay"
)

// PayMethod is the form a payment takes on the paying side. The MVP
// implements Native only; the rest are reserved codes.
type PayMethod int16

const (
	PayMethodNative PayMethod = 1 // 扫码支付
	PayMethodJSAPI  PayMethod = 2 // 公众号 / 小程序（预留）
	PayMethodH5     PayMethod = 3 // H5（预留）
	PayMethodApp    PayMethod = 4 // App（预留）
)

// Supported reports whether the module can initiate this method today.
func (m PayMethod) Supported() bool { return m == PayMethodNative }

// PaymentStatus is the Payment state machine. Terminal states are
// irreversible; every transition is a CAS from Pending and refunds never
// change the status — refunded_amount accumulates independently.
type PaymentStatus int16

const (
	PaymentPending PaymentStatus = 1
	PaymentPaid    PaymentStatus = 2
	PaymentFailed  PaymentStatus = 3
	PaymentClosed  PaymentStatus = 4
	PaymentExpired PaymentStatus = 5
)

// Terminal reports whether the state is final (no further transition may
// change it).
func (s PaymentStatus) Terminal() bool {
	return s == PaymentPaid || s == PaymentFailed || s == PaymentClosed || s == PaymentExpired
}

// Payment is one attempt to collect a stated amount into one Merchant
// Account through one Pay Method. The ID doubles as the Payment number and
// the Channel's merchant order number (out_trade_no). Amount is int64 minor
// units, always (ADR 0019).
type Payment struct {
	ID             int64
	MerchantID     int64
	Channel        Channel
	BizNo          string
	Subject        string
	Amount         int64
	Currency       string
	PayMethod      PayMethod
	Status         PaymentStatus
	ChannelTradeNo string
	ChannelPayload string
	PayerID        string
	RefundedAmount int64
	FailReason     string
	ExpireTime     time.Time
	PaidAt         *time.Time
	CreateBy       int64
	UpdateBy       int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Initiation is the create-time value object: what an initiator states
// before any Channel is contacted.
type Initiation struct {
	MerchantID    int64
	BizNo         string
	Subject       string
	Amount        int64
	Currency      string
	PayMethod     PayMethod
	ExpireMinutes int32
}

// Validate enforces the initiation invariants: opaque-but-present
// references, positive amount, CNY for the MVP channels, a supported pay
// method.
func (i *Initiation) Validate() error {
	if i == nil || i.MerchantID <= 0 || i.BizNo == "" || len(i.BizNo) > 64 ||
		i.Subject == "" || len(i.Subject) > 256 || i.Amount <= 0 {
		return pay.ErrPaymentInvalid
	}
	if i.Currency != "CNY" {
		// The MVP channels settle CNY only; other currencies wait for the
		// channels that speak them (ADR 0019).
		return pay.ErrPaymentInvalid
	}
	if !i.PayMethod.Supported() {
		return pay.ErrPaymentInvalid
	}
	return nil
}

// PaidCapture is the success evidence a Channel confirmation carries.
type PaidCapture struct {
	ChannelTradeNo string
	PayerID        string
	PaidAt         time.Time
	// Amount is the channel-asserted paid total in minor units (ADR 0019);
	// the caller must have verified it against the row's Amount before the
	// settle CAS. Zero means "not asserted".
	Amount int64
}
