package model

// SystemUserCredential replaces the login credential of one System User. The
// password is plaintext at this seam; the provider owns hashing policy and
// the credential column, so no hash format detail ever leaks to consumers.
type SystemUserCredential struct {
	SystemUserID int64
	Password     string
}
