package payinfra

import "glex/internal/infra/pay/crypt"

// NewCipher exposes the process-wide credential cipher to fx (ADR 0018).
// It is constructed from the environment and never fails — an absent
// PAY_MASTER_KEY yields an inert cipher whose crypto calls return the
// master-key sentinel.
func NewCipher() *crypt.Cipher {
	return crypt.New()
}
