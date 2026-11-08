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

// OpenAPI is the domain service's only path to WeChat. Tests script it to
// succeed or fail. The process binds the HTTP adapter.
type OpenAPI interface {
	ProbePlatform(ctx context.Context, req ProbePlatformRequest) error
	AnswerFullNetwork(ctx context.Context, req FullNetworkReply) error
}
