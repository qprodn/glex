package authn

import (
	"context"

	"golang.org/x/crypto/bcrypt"
)

// CredentialRepo is the only writer of the credential column on the System
// User row (ADR 0016: the hash physically stays there until a service split
// forces a dedicated credential table). Writes join the Application
// transaction propagated through ctx.
type CredentialRepo interface {
	// StoreCredential replaces the stored password hash of the System User.
	// It runs inside the caller's transaction alongside the profile write.
	StoreCredential(ctx context.Context, systemUserID int64, passwordHash string) error
}

// HashPassword hashes a plaintext password with the project's bcrypt policy.
// Credentials belong to authn (ADR 0016), so no other module picks the cost.
func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// VerifyPassword checks a plaintext password against a stored bcrypt hash.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// DecoyCredentialHash makes unknown-account verification consume the same
// bcrypt work as a wrong password without depending on any real account.
const DecoyCredentialHash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5jQFKP3N8wJYnAqH.G2L/tW8G8zN2qG"
