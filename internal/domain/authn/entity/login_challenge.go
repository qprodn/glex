package entity

// LoginChallenge is one issued picture proof. The answer stays in the store;
// this value carries only what the caller must render and send back.
type LoginChallenge struct {
	ID    string
	Image []byte
}
