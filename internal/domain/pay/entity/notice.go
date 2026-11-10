package entity

import "time"

// NoticeKind selects which transition a Business Notification announces.
// A payment notice and a refund notice are different records.
type NoticeKind int16

const (
	NoticeKindPayment NoticeKind = 1
	NoticeKindRefund  NoticeKind = 2
)

// NoticeStatus is the automatic-delivery state of one Business Notification.
// Waiting has attempts left. Accepted means a delivery was taken. Exhausted
// means the automatic schedule finished without acceptance.
type NoticeStatus int16

const (
	NoticeWaiting   NoticeStatus = 1
	NoticeAccepted  NoticeStatus = 2
	NoticeExhausted NoticeStatus = 3
)

// BusinessNotice is one Business Notification. It is not a Channel
// Notification and it carries no signature. The copied address, the
// Business Order Number, and the Payment are fixed when the notice is
// recorded. Automatic delivery is a later step; this record is what that
// step finds.
type BusinessNotice struct {
	ID            int64
	ApplicationID int64
	Kind          NoticeKind
	PaymentID     int64
	RefundID      int64
	BizNo         string
	NotifyURL     string
	Status        NoticeStatus
	AttemptCount  int
	NextAttemptAt time.Time
	CreateBy      int64
	UpdateBy      int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
