package service

import (
	"context"

	"glex/internal/domain/wechat/entity"
)

// ProbePlatformRequest is one WeChat Connectivity Test against a platform
// registration: the component AppID, the component secret, and the verify
// ticket the platform currently holds. The port classifies WeChat's answer.
// Which WeChat codes are a definitive refusal is left unset (the port is
// the classification boundary) and is not this call's concern: a platform
// probe never changes a WeChat App.
type ProbePlatformRequest struct {
	ComponentAppID  string
	ComponentSecret string
	VerifyTicket    string
}

// FullNetworkReply is the published customer-service answer for one
// full-network release QUERY_AUTH_CODE push. The port delivers it and
// stores nothing: no WeChat App, no WeChat Dispatch.
type FullNetworkReply struct {
	ComponentAppID  string
	ComponentSecret string
	VerifyTicket    string
	QueryAuthCode   string
	ToUser          string
	Content         string
}

// OpenAuthorizationStart asks WeChat to begin a Platform Authorization for
// one ready platform. RedirectURL is where the authorizing administrator's
// browser returns. The result is the link the operator hands to that
// administrator. The call does not create a WeChat App.
type OpenAuthorizationStart struct {
	ComponentAppID  string
	ComponentSecret string
	VerifyTicket    string
	RedirectURL     string
}

// AuthorizationLink is the operator-visible start of a Platform
// Authorization: the URL the other administrator opens.
type AuthorizationLink struct {
	URL string
}

// OpenAuthorizationCode exchanges one completed grant. The port reports
// what WeChat returned. Permission-set numbers are not part of this
// report; Capabilities are the names of this phase's capabilities.
type OpenAuthorizationCode struct {
	ComponentAppID  string
	ComponentSecret string
	VerifyTicket    string
	AuthCode        string
}

// AuthorizationGrant is one completed Platform Authorization as WeChat
// reported it. Kind is empty when WeChat did not report Service Account
// or Subscription Account. RefreshToken is the authorizer refresh
// material, never an authorizer AppSecret.
type AuthorizationGrant struct {
	AppID        string
	Name         string
	AppType      string
	Kind         string
	Capabilities []string
	RefreshToken string
}

// CallClass is how the Open API port classifies one WeChat call. The
// domain service does not inspect WeChat error codes. Only
// CallDefinitiveRefusal lapses a WeChat App. Which codes fall into which
// class is the port's concern and is left unset.
type CallClass int

const (
	// CallSuccess is a call WeChat accepted. An app connectivity test
	// that succeeds changes no suspension, lapse, or connection.
	CallSuccess CallClass = iota
	// CallBusinessRejection is an ordinary WeChat refusal, such as a
	// person who has not subscribed. The app stays unlapsed.
	CallBusinessRejection
	// CallUnavailable is a timeout or WeChat being unreachable. The app
	// stays unlapsed.
	CallUnavailable
	// CallDefinitiveRefusal is WeChat rejecting the grant or the Direct
	// Hold secret. The app lapses.
	CallDefinitiveRefusal
)

// ProbeAppRequest is one WeChat Connectivity Test of an app's current
// token. A Direct Hold carries AppID and AppSecret. A Platform
// Authorization carries the platform registration and the authorizer
// refresh material, and no authorizer AppSecret. The call sends no
// message and creates no WeChat Dispatch.
type ProbeAppRequest struct {
	Connection             string
	AppID                  string
	AppSecret              string
	ComponentAppID         string
	ComponentSecret        string
	VerifyTicket           string
	AuthorizerRefreshToken string
}

// ProbeAppResult is the port's classification of that test. Message is
// operator-readable and is not a WeChat error code. AuthorizerRefreshToken
// is set when WeChat issued a replacement during a successful platform
// test; empty means the stored material stays.
type ProbeAppResult struct {
	Class                  CallClass
	Message                string
	AuthorizerRefreshToken string
}

// AdoptLibraryTemplateRequest asks WeChat to take one public-library
// template onto the app whose current token the request carries. The
// port classifies the answer. This module does not invent a template.
type AdoptLibraryTemplateRequest struct {
	ProbeAppRequest
	LibraryTemplateID string
	Channel           string
}

// AdoptLibraryTemplateResult is the port's classification of that
// adoption. WechatTemplateID is the id WeChat assigned on this app.
// Title is the operator-visible name WeChat returned. Both are empty
// when the call was not accepted.
type AdoptLibraryTemplateResult struct {
	Class                  CallClass
	Message                string
	WechatTemplateID       string
	Title                  string
	AuthorizerRefreshToken string
}

// SendMessageRequest is one synchronous send of an adopted template to
// one openid. The port classifies WeChat's answer. MsgID is set only
// when WeChat accepts. WebURL and MiniProgramPage are both handed over
// when the operator set both. MiniProgramAppID travels with the page so
// WeChat can open that mini program.
type SendMessageRequest struct {
	ProbeAppRequest
	Channel          string
	OpenID           string
	WechatTemplateID string
	Content          string
	WebURL           string
	MiniProgramAppID string
	MiniProgramPage  string
}

// SendMessageResult is the port's classification of that send.
type SendMessageResult struct {
	Class                  CallClass
	Message                string
	MsgID                  string
	AuthorizerRefreshToken string
}

// PublishMenuCall replaces one Service Account's default menu with the
// whole saved document. The port classifies WeChat's answer. A Direct
// Hold carries AppID and AppSecret. A Platform Authorization carries the
// platform registration and the authorizer refresh material.
type PublishMenuCall struct {
	ProbeAppRequest
	Menu entity.CustomMenu
}

// PublishMenuResult is the port's classification of that replacement.
// Message is operator-readable and is not a WeChat error code.
// AuthorizerRefreshToken is set when WeChat issued a replacement during
// the call; empty means the stored material stays.
type PublishMenuResult struct {
	Class                  CallClass
	Message                string
	AuthorizerRefreshToken string
}

// MiniProgramLoginCall is one exchange of a mini program's one-time login
// code. A Direct Hold carries AppID and AppSecret. A Platform
// Authorization carries the platform registration and no authorizer
// AppSecret. The port drops session_key: it is not a field here.
type MiniProgramLoginCall struct {
	ProbeAppRequest
	Code string
}

// MiniProgramLoginResult is the port's classification of that exchange.
// OpenID is set when WeChat accepted the code. UnionID is empty when
// WeChat omitted it. session_key is not returned.
type MiniProgramLoginResult struct {
	Class   CallClass
	OpenID  string
	UnionID string
}

// OpenAPI is the domain service's only path to WeChat. Tests script it to
// succeed or fail. The process binds the HTTP adapter.
type OpenAPI interface {
	ProbePlatform(ctx context.Context, req ProbePlatformRequest) error
	ProbeApp(ctx context.Context, req ProbeAppRequest) (ProbeAppResult, error)
	PublishMenu(ctx context.Context, req PublishMenuCall) (PublishMenuResult, error)
	MiniProgramLogin(ctx context.Context, req MiniProgramLoginCall) (MiniProgramLoginResult, error)
	AnswerFullNetwork(ctx context.Context, req FullNetworkReply) error
	StartAuthorization(ctx context.Context, req OpenAuthorizationStart) (*AuthorizationLink, error)
	CompleteAuthorization(ctx context.Context, req OpenAuthorizationCode) (*AuthorizationGrant, error)
	AdoptLibraryTemplate(ctx context.Context, req AdoptLibraryTemplateRequest) (AdoptLibraryTemplateResult, error)
	SendMessage(ctx context.Context, req SendMessageRequest) (SendMessageResult, error)
	// ExchangeWebAuthorization turns the code WeChat just issued into an
	// identity. Profile asks for nickname and avatar. The access token
	// stays inside the port. session_key is not part of this call.
	ExchangeWebAuthorization(ctx context.Context, req ExchangeWebAuthorizationRequest) (WebAuthorizationExchange, error)
}

// ExchangeWebAuthorizationRequest is one completed Web Authorization code.
// Profile is true when the app asked for nickname and avatar.
type ExchangeWebAuthorizationRequest struct {
	ProbeAppRequest
	Code    string
	Profile bool
}

// WebAuthorizationExchange is the identity WeChat returned, already
// classified. Nickname and AvatarURL are meaningful only when the call
// asked for them. The domain still decides what a redeem returns.
type WebAuthorizationExchange struct {
	Class     CallClass
	OpenID    string
	UnionID   string
	Nickname  string
	AvatarURL string
	Message   string
}
