package biz

import "golang.org/x/crypto/bcrypt"

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
