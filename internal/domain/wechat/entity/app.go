package entity

import "time"

const (
	// AppTypeMiniProgram is a mini program (小程序). It has no account kind.
	AppTypeMiniProgram = "mini_program"
	// AppTypeOfficialAccount is an official account (公众号).
	AppTypeOfficialAccount = "official_account"
	// AccountKindService is a Service Account (服务号).
	AccountKindService = "service_account"
	// AccountKindSubscription is a Subscription Account (订阅号).
	AccountKindSubscription = "subscription_account"
	// ConnectionDirectHold is a WeChat App this system holds directly
	// (直连). The connection is fixed for the life of the row (ADR 0029).
	ConnectionDirectHold = "direct_hold"
)

// App is one WeChat App (微信应用). Ordinary reads carry masks for the
// AppSecret and the Push Secret and never the plaintext (ADR 0029). The
// generated Token and EncodingAESKey travel beside this object only in
// the response that generated them. CallbackAddress is derived from the
// row id so each Direct Hold shows its own.
type App struct {
	ID                 int64
	Name               string
	AppID              string
	AppType            string
	Kind               string
	Connection         string
	AppSecretMask      string
	TokenMask          string
	EncodingAESKeyMask string
	CallbackAddress    string
	Suspended          bool
	KeyVersion         int
	CreateBy           int64
	UpdateBy           int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
