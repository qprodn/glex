package service

import "context"

// AuthenticateRequest is one inbound push plus the platform's messaging
// secrets. The authenticator accepts only WeChat's encrypted mode.
type AuthenticateRequest struct {
	Token          string
	EncodingAESKey string
	ComponentAppID string
	Timestamp      string
	Nonce          string
	Signature      string
	MsgSignature   string
	EncryptType    string
	Body           []byte
}

// AuthenticatedPush is the decrypted XML of an encrypted push. Plaintext,
// compatible mode, and a failed verification never produce one.
type AuthenticatedPush struct {
	XML []byte
}

// SealReplyRequest is a callback body WeChat's encrypted mode must receive
// back as ciphertext. An empty Plaintext is the published empty answer and
// is not sealed.
type SealReplyRequest struct {
	Token          string
	EncodingAESKey string
	ComponentAppID string
	Nonce          string
	Plaintext      string
}

// PushAuthenticator is the real collaborator that decides whether a push
// is an encrypted message from WeChat. Tests use the production
// implementation, not a script.
type PushAuthenticator interface {
	Authenticate(ctx context.Context, req AuthenticateRequest) (*AuthenticatedPush, error)
	SealReply(ctx context.Context, req SealReplyRequest) (string, error)
}
