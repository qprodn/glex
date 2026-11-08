package repository

import "context"

// HandoffGrant is the identity one Web Authorization yielded. It is not a
// WeChat User, a Credential, or a Login Session. Nickname and AvatarURL
// are set only when that app asked for them. Profile records that choice
// so a later redeem cannot grow a scope the grant did not have.
type HandoffGrant struct {
	OpenID    string
	UnionID   string
	Nickname  string
	AvatarURL string
	Profile   bool
}

// HandoffCodeRepo stores Handoff Codes the way a Login Challenge is
// stored: single-use and ephemeral, and not a user. Issue returns the
// code. Consume removes the grant, so a second consume cannot observe
// it. An unknown code is ErrHandoffCodeInvalid. This ticket sets no
// lifetime; single-use is the whole rule.
type HandoffCodeRepo interface {
	Issue(ctx context.Context, grant HandoffGrant) (string, error)
	Consume(ctx context.Context, code string) (HandoffGrant, error)
}
