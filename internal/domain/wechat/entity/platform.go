// Package entity holds the wechat module's domain objects.
package entity

import "time"

// KeyVersion is the master-key version recorded beside ciphertext
// (ADR 0018). Rotation is not performed here; the column exists so a
// later version can rewrite rows.
const KeyVersion = 1

// Platform is one WeChat Platform registration (微信平台). Ordinary reads
// carry masks for the component secret, Token, and EncodingAESKey and
// never the plaintext (ADR 0029). The component AppID is immutable for
// the life of the row.
type Platform struct {
	ID                  int64
	Name                string
	ComponentAppID      string
	ComponentSecretMask string
	TokenMask           string
	EncodingAESKeyMask  string
	KeyVersion          int
	CreateBy            int64
	UpdateBy            int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
