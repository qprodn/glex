package entity

import "time"

// AdoptedTemplate is one template taken from WeChat's public library onto
// one WeChat App. This module does not invent templates. The same library
// template is not adopted twice onto the same live app.
type AdoptedTemplate struct {
	ID                int64
	AppID             int64
	LibraryTemplateID string
	WechatTemplateID  string
	Title             string
	// Channel is the capability this library entry fills: a Template
	// Message, a Subscribe Notification, or a Subscribe Message.
	Channel   string
	CreateBy  int64
	UpdateBy  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
