package biz

import "golang.org/x/crypto/bcrypt"

// invalidCredentialHash makes unknown-user verification consume the same
// bcrypt work as a wrong password without depending on any real System User.
const invalidCredentialHash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5jQFKP3N8wJYnAqH.G2L/tW8G8zN2qG"

// hashPassword hashes a plaintext password using bcrypt. Used by the user
// usecase at the biz layer (not at the transport boundary), so callers cannot
// accidentally persist plaintext by skipping a controller.
func hashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func passwordMatches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
