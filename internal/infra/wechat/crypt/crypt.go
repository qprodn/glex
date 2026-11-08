// Package crypt implements the credential envelope of ADR 0018 for the
// wechat module: platform secrets are sealed whole with AES-256-GCM under
// a single master key, a fresh nonce per write, and a key_version prefix
// so the key can later rotate without a flag day. The master key is 32
// bytes of base64 from the environment (WECHAT_MASTER_KEY) — never
// config.yaml, and not PAY_MASTER_KEY (ADR 0029). An absent or malformed
// key yields a cipher whose every crypto call returns ErrMasterKeyMissing:
// platform registration refuses to operate rather than degrade to plaintext.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// EnvMasterKey names the environment variable the master key arrives in.
const EnvMasterKey = "WECHAT_MASTER_KEY"

// KeyVersion is the KEK version written into every token. A new version
// means a new prefix, a rewrite, then retirement of the old key (ADR 0018).
const KeyVersion = 1

// ErrMasterKeyMissing is returned by every crypto call while the master
// key is absent or malformed. Callers map it to the module's
// WECHAT_MASTER_KEY_MISSING reason.
var ErrMasterKeyMissing = errors.New("crypt: WECHAT_MASTER_KEY is not configured or malformed")

const (
	nonceSize  = 12
	aesKeySize = 32
)

// Cipher seals and opens wechat secret value sets.
type Cipher struct {
	key []byte
}

// New builds the process-wide cipher from the environment. It never fails:
// a missing or malformed key produces an inert cipher that reports
// ErrMasterKeyMissing on use (unrelated paths — login and kin — are
// unaffected).
func New() *Cipher {
	raw := strings.TrimSpace(os.Getenv(EnvMasterKey))
	if raw == "" {
		return &Cipher{}
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != aesKeySize {
		return &Cipher{}
	}
	return &Cipher{key: key}
}

// Available reports whether a usable master key is configured.
func (c *Cipher) Available() bool { return len(c.key) == aesKeySize }

// Encrypt seals the value set into the stored token
// "key_version:nonce_base64:ciphertext_base64".
func (c *Cipher) Encrypt(values map[string]string) (string, error) {
	aesgcm, err := c.aead()
	if err != nil {
		return "", err
	}
	plaintext, err := canonical(values)
	if err != nil {
		return "", fmt.Errorf("crypt: marshal secrets: %w", err)
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypt: read nonce: %w", err)
	}
	sealed := aesgcm.Seal(nil, nonce, plaintext, nil)
	return fmt.Sprintf("%d:%s:%s",
		KeyVersion,
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(sealed),
	), nil
}

// Decrypt opens a stored token back into the value set. Tokens sealed
// under an unsupported key_version are refused (rotation rewrites rows
// before the old key retires).
func (c *Cipher) Decrypt(token string) (map[string]string, error) {
	aesgcm, err := c.aead()
	if err != nil {
		return nil, err
	}
	parts := strings.Split(token, ":")
	if len(parts) != 3 {
		return nil, errors.New("crypt: malformed ciphertext token")
	}
	var version int
	if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil || version != KeyVersion {
		return nil, fmt.Errorf("crypt: unsupported key version %q", parts[0])
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(nonce) != nonceSize {
		return nil, errors.New("crypt: malformed nonce")
	}
	sealed, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("crypt: malformed ciphertext")
	}
	plaintext, err := aesgcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, errors.New("crypt: ciphertext authentication failed")
	}
	var values map[string]string
	if err := json.Unmarshal(plaintext, &values); err != nil {
		return nil, fmt.Errorf("crypt: unmarshal secrets: %w", err)
	}
	return values, nil
}

func (c *Cipher) aead() (cipher.AEAD, error) {
	if !c.Available() {
		return nil, ErrMasterKeyMissing
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("crypt: new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypt: new gcm: %w", err)
	}
	return aesgcm, nil
}

// canonical renders the value set as deterministic JSON (encoding/json
// sorts map keys), the form the seal is taken over.
func canonical(values map[string]string) ([]byte, error) {
	if values == nil {
		values = map[string]string{}
	}
	return json.Marshal(values)
}
