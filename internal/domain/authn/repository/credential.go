package repository

import (
	"context"
)

// StoreCredentialRequest replaces the stored password hash of one System User
// (req.SystemUserID) with req.PasswordHash.
type StoreCredentialRequest struct {
	SystemUserID int64
	PasswordHash string
}

// CredentialRepo is the only writer of the credential column on the System
// User row (ADR 0005: the hash physically stays there until a service split
// forces a dedicated credential table). Writes join the Application
// transaction propagated through ctx.
type CredentialRepo interface {
	// StoreCredential replaces the stored password hash of the System User.
	// It runs inside the caller's transaction alongside the profile write.
	StoreCredential(ctx context.Context, req StoreCredentialRequest) error
}
