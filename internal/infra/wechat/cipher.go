package wechatinfra

import "glex/internal/infra/wechat/crypt"

// NewCipher builds the process-wide wechat cipher from WECHAT_MASTER_KEY.
// A missing key does not fail construction; crypto calls refuse instead of
// storing plaintext (ADR 0029).
func NewCipher() *crypt.Cipher {
	return crypt.New()
}
