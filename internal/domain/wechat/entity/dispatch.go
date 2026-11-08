package entity

import "time"

// Dispatch is one WeChat Dispatch: a single attempt to send one Adopted
// Template to one openid. Sending again is a new row. Withdrawing the
// app does not withdraw the dispatch. Accepted is WeChat's acceptance of
// this attempt; a rejected attempt has no msgid. Delivered is a later
// delivery report for that msgid. Acceptance is not delivery. A timeout
// is not a row, and a report that matches nothing does not create one.
type Dispatch struct {
	ID                int64
	AppID             int64
	OpenID            string
	TemplateID        int64
	LibraryTemplateID string
	WechatTemplateID  string
	Channel           string
	Content           string
	WebURL            string
	MiniProgramPage   string
	Accepted          bool
	Delivered         bool
	MsgID             string
	CreateBy          int64
	UpdateBy          int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
