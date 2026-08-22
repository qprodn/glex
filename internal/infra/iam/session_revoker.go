package iaminfra

import "glex/internal/domain/iam"

// NewSessionRevoker narrows the Login Session repository to the revocation
// interface consumed by security-changing application operations.
func NewSessionRevoker(sessions iam.LoginSessionRepo) iam.SessionRevoker {
	return sessions
}
