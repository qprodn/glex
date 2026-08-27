// Package service holds the authn module's own rules and intra-domain
// orchestration: credential hashing policy, Principal context plumbing, and
// login-failure normalization. Cross-domain orchestration (transactions,
// authorization resolution) stays with the Application services.
package service

import (
	"golang.org/x/crypto/bcrypt"
)

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
