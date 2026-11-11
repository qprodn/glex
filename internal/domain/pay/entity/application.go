package entity

import "time"

// ApplicationStatus is the Pay Application switch: disabled applications
// cannot back new Payments or Refunds; in-flight ones still settle.
type ApplicationStatus int16

const (
	ApplicationEnabled  ApplicationStatus = 0
	ApplicationDisabled ApplicationStatus = 1
)

// PayApplication is the caller's identity in the pay module. It is named by
// its own number and by a display name, and it is bound at creation to
// exactly one Merchant Account (ADR 0030). There is no app key and no secret.
type PayApplication struct {
	ID              int64
	Name            string
	MerchantID      int64
	Status          ApplicationStatus
	OrderNotifyURL  string
	RefundNotifyURL string
	CreateBy        int64
	UpdateBy        int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
