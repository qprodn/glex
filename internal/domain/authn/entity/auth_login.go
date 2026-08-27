package entity

// LoginAccount is the authentication view of a System User account: the login
// identifier, the stored credential hash, and the status gate. It is the only
// shape through which login reads the credential column (ADR 0016); profile
// fields stay with the user module.
type LoginAccount struct {
	ID           int64
	UserName     string
	PasswordHash string
	Status       int32
}
