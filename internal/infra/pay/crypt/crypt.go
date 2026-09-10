// Package crypt implements the credential envelope of ADR 0018: channel
// configs are sealed whole with AES-256-GCM under a single master key, a
// fresh nonce per write, and a key_version prefix so the key can later
// rotate or move to a KMS without a flag day. The master key is 32 bytes
// of base64 from the environment (PAY_MASTER_KEY) — never config.yaml.
// An absent or malformed key yields a cipher whose every crypto call
// returns ErrMasterKeyMissing: merchant configuration refuses to operate
// rather than degrade to plaintext.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// EnvMasterKey names the environment variable the master key arrives in.
const EnvMasterKey = "PAY_MASTER_KEY"

// ErrMasterKeyMissing is returned by every crypto call while the master
// key is absent or malformed. Callers map it to the module's
// MASTER_KEY_MISSING reason.
var ErrMasterKeyMissing = errors.New("crypt: PAY_MASTER_KEY is not configured or malformed")

const (
	keyVersion  = 1 // KEK rotation reserve: new version ⇒ new prefix, rewrite, retire
	nonceSize   = 12
	aesKeySize  = 32
)

// Cipher seals and opens merchant config value sets.
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
		return "", fmt.Errorf("crypt: marshal config: %w", err)
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypt: read nonce: %w", err)
	}
	sealed := aesgcm.Seal(nil, nonce, plaintext, nil)
	return fmt.Sprintf("%d:%s:%s",
		keyVersion,
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
	if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil || version != keyVersion {
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
		return nil, fmt.Errorf("crypt: unmarshal config: %w", err)
	}
	return values, nil
}

// Digest is the sha256 hex of the canonical plaintext config — the stable
// summary the gateway cache keys on; it changes whenever the config is
// rewritten.
func (c *Cipher) Digest(values map[string]string) string {
	b, err := canonical(values)
	if err != nil {
		// canonical only fails on unencodable value content, which string
		// maps cannot produce; the panic path is unreachable in practice.
		b = []byte("{}")
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
// sorts map keys), the form both the seal and the digest are taken over.
func canonical(values map[string]string) ([]byte, error) {
	if values == nil {
		values = map[string]string{}
	}
	return json.Marshal(values)
}
