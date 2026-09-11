package crypt

import (
	"encoding/base64"
	"strings"
	"testing"
)

// testKey is a fixed 32-byte master key (base64) for the round-trip tests.
var testKey = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	t.Setenv(EnvMasterKey, testKey)
	return New()
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	values := map[string]string{
		"mchid":                "1900000001",
		"merchant_private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvQ\n-----END PRIVATE KEY-----",
		"api_v3_key":           "a1b2c3d4e5f6g7h8a1b2c3d4e5f6g7h8",
	}
	token, err := c.Encrypt(values)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Token shape: key_version:nonce_base64:ciphertext_base64 (ADR 0018).
	parts := strings.Split(token, ":")
	if len(parts) != 3 {
		t.Fatalf("token = %q, want 3 colon-separated parts", token)
	}
	if parts[0] != "1" {
		t.Fatalf("key version = %q, want 1", parts[0])
	}
	if _, err := base64.StdEncoding.DecodeString(parts[1]); err != nil {
		t.Fatalf("nonce is not base64: %v", err)
	}
	decrypted, err := c.Decrypt(token)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	for k, want := range values {
		if decrypted[k] != want {
			t.Fatalf("decrypted[%q] = %q, want %q", k, decrypted[k], want)
		}
	}
}

func TestEncryptFreshNoncePerWrite(t *testing.T) {
	c := newTestCipher(t)
	values := map[string]string{"mchid": "1900000001"}
	first, err := c.Encrypt(values)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	second, err := c.Encrypt(values)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if first == second {
		t.Fatal("identical plaintext sealed to identical tokens — nonce reused")
	}
}

func TestTamperedCiphertextFailsAuth(t *testing.T) {
	c := newTestCipher(t)
	token, err := c.Encrypt(map[string]string{"mchid": "1900000001"})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	parts := strings.Split(token, ":")
	sealed, _ := base64.StdEncoding.DecodeString(parts[2])
	sealed[0] ^= 0xFF
	tampered := parts[0] + ":" + parts[1] + ":" + base64.StdEncoding.EncodeToString(sealed)
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("tampered ciphertext decrypted without error")
	}
}

func TestDecryptRejectsUnknownKeyVersion(t *testing.T) {
	c := newTestCipher(t)
	if _, err := c.Decrypt("99:AAAA:AAAA"); err == nil {
		t.Fatal("unknown key version accepted")
	}
}

func TestDigestStableAndSensitive(t *testing.T) {
	c := newTestCipher(t)
	values := map[string]string{"mchid": "1", "appid": "wx1"}
	// Map iteration order must not leak into the digest.
	other := map[string]string{"appid": "wx1", "mchid": "1"}
	if c.Digest(values) != c.Digest(other) {
		t.Fatal("digest differs for identical configs")
	}
	changed := map[string]string{"mchid": "1", "appid": "wx2"}
	if c.Digest(values) == c.Digest(changed) {
		t.Fatal("digest identical for different configs")
	}
	if len(c.Digest(values)) != 64 {
		t.Fatalf("digest length = %d, want sha256 hex (64)", len(c.Digest(values)))
	}
	if c.Digest(nil) != c.Digest(map[string]string{}) {
		t.Fatal("nil and empty configs must digest identically")
	}
}

func TestMissingMasterKeyIsInertNotFatal(t *testing.T) {
	t.Setenv(EnvMasterKey, "")
	c := New()
	if c.Available() {
		t.Fatal("cipher available without a key")
	}
	if _, err := c.Encrypt(map[string]string{"mchid": "1"}); err != ErrMasterKeyMissing {
		t.Fatalf("encrypt err = %v, want ErrMasterKeyMissing", err)
	}
	if _, err := c.Decrypt("1:AAAA:AAAA"); err != ErrMasterKeyMissing {
		t.Fatalf("decrypt err = %v, want ErrMasterKeyMissing", err)
	}
}

func TestMalformedMasterKeyIsInert(t *testing.T) {
	t.Setenv(EnvMasterKey, "not-base64!!!")
	if c := New(); c.Available() {
		t.Fatal("cipher available with malformed key")
	}
	t.Setenv(EnvMasterKey, base64.StdEncoding.EncodeToString([]byte("short")))
	if c := New(); c.Available() {
		t.Fatal("cipher available with short key")
	}
}
