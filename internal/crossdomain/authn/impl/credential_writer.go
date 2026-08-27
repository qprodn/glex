package impl

import (
	"context"
	"fmt"

	"glex/internal/crossdomain/authn"
	"glex/internal/crossdomain/authn/model"
	authnrepo "glex/internal/domain/authn/repository"
	authnservice "glex/internal/domain/authn/service"
)

// credentialWriter satisfies the credential contract by wrapping the
// credential repository: hashing happens here, beside the provider's domain
// policy, so consumers never pick a hash format.
type credentialWriter struct {
	credentials authnrepo.CredentialRepo
}

// NewCredentialWriter adapts the credential repository to the crossdomain
// credential contract.
func NewCredentialWriter(credentials authnrepo.CredentialRepo) authn.CredentialWriter {
	return &credentialWriter{credentials: credentials}
}

// WriteSystemUserCredential hashes the requested password with the provider's
// policy and stores it inside the caller's propagated transaction.
func (w *credentialWriter) WriteSystemUserCredential(ctx context.Context, request *model.SystemUserCredential) error {
	if request == nil || request.SystemUserID <= 0 {
		return fmt.Errorf("write system user credential: system_user_id is required")
	}
	hash, err := authnservice.HashPassword(request.Password)
	if err != nil {
		return fmt.Errorf("write system user credential: hash password: %w", err)
	}
	return w.credentials.StoreCredential(ctx, request.SystemUserID, hash)
}
