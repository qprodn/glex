// Package authn publishes authn's cross-module contracts in the anti-corruption
// shape of ADR 0017: contract.go holds the consumer-shaped interface, model/
// holds flat snapshot types, impl/ adapts the provider's domain capability, and
// authnmock holds the mockgen-generated test double. Contracts are consumed by
// constructor injection into the calling Usecase; there is deliberately no
// package-level service locator, and cmd stays the only composition root.
package authn

import (
	"context"

	"glex/internal/crossdomain/authn/model"
)

//go:generate go tool mockgen -package authnmock -destination authnmock/session_revoker.go glex/internal/crossdomain/authn SessionRevoker
//go:generate go tool mockgen -package authnmock -destination authnmock/credential_writer.go glex/internal/crossdomain/authn CredentialWriter

// SessionRevoker revokes Login Sessions after a security-changing write in a
// consuming module. The request is a flat model snapshot; the provider's
// domain types never cross this seam.
type SessionRevoker interface {
	RevokeSystemUsers(ctx context.Context, request *model.RevocationRequest) error
}

// CredentialWriter replaces a System User's login credential (ADR 0016: the
// credential and its hashing policy belong to authn, and only authn's adapter
// writes the credential column). The plaintext password crosses this seam;
// writing a credential does not itself revoke Login Sessions — callers
// classify and revoke through SessionRevoker.
type CredentialWriter interface {
	WriteSystemUserCredential(ctx context.Context, request *model.SystemUserCredential) error
}
