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
	// ConnectionPlatformAuthorization is a WeChat App granted through one
	// WeChat Platform (平台授权). The connection and the platform binding
	// are fixed for the life of the row (ADR 0029).
	ConnectionPlatformAuthorization = "platform_authorization"

	// CapabilityCustomMenu is a Custom Menu grant.
	CapabilityCustomMenu = "custom_menu"
	// CapabilityTemplateMessage is a Template Message grant.
	CapabilityTemplateMessage = "template_message"
	// CapabilitySubscribeNotification is a Subscribe Notification grant.
	CapabilitySubscribeNotification = "subscribe_notification"
	// CapabilityWebAuthorization is a Web Authorization grant.
	CapabilityWebAuthorization = "web_authorization"
	// CapabilityMiniProgramLogin is a Mini Program Login grant.
	CapabilityMiniProgramLogin = "mini_program_login"
	// CapabilitySubscribeMessage is a Subscribe Message grant.
	CapabilitySubscribeMessage = "subscribe_message"

	// WebAuthScopeOpenID asks a Web Authorization for openid only. unionid
	// is returned when WeChat supplies one. This is the default.
	WebAuthScopeOpenID = "openid"
	// WebAuthScopeProfile also asks for the nickname and avatar.
	WebAuthScopeProfile = "profile"
)

// MenuButton is one button of a Custom Menu. A parent holds sub-buttons
// and does not itself open a page. A leaf opens a web page or a mini
// program.
type MenuButton struct {
	Name             string       `json:"name,omitempty"`
	WebURL           string       `json:"web_url,omitempty"`
	MiniProgramAppID string       `json:"mini_program_app_id,omitempty"`
	MiniProgramPage  string       `json:"mini_program_page,omitempty"`
	SubButtons       []MenuButton `json:"sub_buttons,omitempty"`
}

// CustomMenu is the default menu document of one Service Account,
// including nested sub-buttons. The prototype's ceiling of three
// top-level buttons is not a rule of this document.
type CustomMenu struct {
	Buttons []MenuButton `json:"buttons,omitempty"`
}

// App is one WeChat App (微信应用). Ordinary reads carry masks for the
// AppSecret and the Push Secret and never the plaintext (ADR 0029). The
// generated Token and EncodingAESKey travel beside this object only in
// the response that generated them. CallbackAddress is derived from the
// row id so each Direct Hold shows its own. A Platform Authorization has
// no authorizer AppSecret and no Push Secret; it is bound to PlatformID
// and records the capabilities WeChat granted. Those names are this
// phase's capabilities, not WeChat permission-set numbers.
type App struct {
	ID                 int64
	Name               string
	AppID              string
	AppType            string
	Kind               string
	Connection         string
	PlatformID         int64
	Capabilities       []string
	AppSecretMask      string
	TokenMask          string
	EncodingAESKeyMask string
	CallbackAddress    string
	Suspended          bool
	// Lapsed is set only when WeChat definitively refuses this app: the
	// Platform Authorization was cancelled, or the Direct Hold secret was
	// rejected. A timeout, a business rejection, and an unready platform
	// do not set it. A replacement AppSecret, or a new grant on the same
	// platform, clears it. Suspension is a separate flag.
	Lapsed bool
	// ReturnURL is where the browser lands after WeChat accepts a Web
	// Authorization. It is stored on the Service Account. A request
	// cannot substitute another address.
	ReturnURL string
	// WebAuthScope is openid only, or openid plus nickname and avatar.
	// A new Service Account starts at openid only.
	WebAuthScope string
	// SavedMenu is the Custom Menu kept in this system. Saving it does
	// not call WeChat. Nil means the operator has not saved one.
	SavedMenu *CustomMenu
	// AcceptedMenu is the last menu WeChat accepted as this Service
	// Account's default menu. A refused or timed-out publish leaves it
	// unchanged. Nil means WeChat has not accepted one.
	AcceptedMenu *CustomMenu
	KeyVersion   int
	CreateBy     int64
	UpdateBy     int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
