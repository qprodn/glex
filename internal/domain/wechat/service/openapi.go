package service

import "context"

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

// OpenAPI is the domain service's only path to WeChat. Tests script it to
// succeed or fail. The process binds the HTTP adapter.
type OpenAPI interface {
	ProbePlatform(ctx context.Context, req ProbePlatformRequest) error
	AnswerFullNetwork(ctx context.Context, req FullNetworkReply) error
	StartAuthorization(ctx context.Context, req OpenAuthorizationStart) (*AuthorizationLink, error)
	CompleteAuthorization(ctx context.Context, req OpenAuthorizationCode) (*AuthorizationGrant, error)
}
