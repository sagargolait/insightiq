package crypto

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func key(b byte) []byte {
	k := make([]byte, keySize)
	for i := range k {
		k[i] = b
	}
	return k
}

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := New(map[string][]byte{"v1": key(0x01)}, "v1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	const secret = "s3cr3t-p@ssw0rd"

	envelope, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !strings.HasPrefix(envelope, "v1:") {
		t.Errorf("envelope %q missing version prefix", envelope)
	}
	if strings.Contains(envelope, secret) {
		t.Error("envelope leaked plaintext")
	}

	got, err := c.Decrypt(envelope)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != secret {
		t.Errorf("Decrypt = %q, want %q", got, secret)
	}
}

func TestEncryptIsNondeterministic(t *testing.T) {
	c := newTestCipher(t)
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Error("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	enc := newTestCipher(t)
	envelope, _ := enc.Encrypt("secret")

	// A cipher holding version v1 but with different key material.
	wrong, err := New(map[string][]byte{"v1": key(0x02)}, "v1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := wrong.Decrypt(envelope); !errors.Is(err, ErrMalformedCiphertext) {
		t.Errorf("wrong key should fail auth, got %v", err)
	}
}

func TestDecryptUnknownVersionFails(t *testing.T) {
	c := newTestCipher(t)
	if _, err := c.Decrypt("v9:" + base64.StdEncoding.EncodeToString([]byte("whatever"))); !errors.Is(err, ErrUnknownKeyVersion) {
		t.Errorf("unknown version should fail, got %v", err)
	}
}

func TestDecryptTamperedFails(t *testing.T) {
	c := newTestCipher(t)
	envelope, _ := c.Encrypt("secret")

	version, encoded, _ := strings.Cut(envelope, ":")
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	raw[len(raw)-1] ^= 0xFF // flip a ciphertext bit
	tampered := version + ":" + base64.StdEncoding.EncodeToString(raw)

	if _, err := c.Decrypt(tampered); !errors.Is(err, ErrMalformedCiphertext) {
		t.Errorf("tampered ciphertext should fail, got %v", err)
	}
}

func TestDecryptMalformedFails(t *testing.T) {
	c := newTestCipher(t)
	for _, bad := range []string{"", "noseparator", "v1:", ":abc", "v1:not-base64!!"} {
		if _, err := c.Decrypt(bad); err == nil {
			t.Errorf("Decrypt(%q) should fail", bad)
		}
	}
}

func TestKeyRotationDecryptsOldCiphertext(t *testing.T) {
	// Seal under v1, then build a cipher whose active version is v2 but which
	// still holds v1. The old ciphertext must still open.
	old, _ := New(map[string][]byte{"v1": key(0x01)}, "v1")
	envelope, _ := old.Encrypt("legacy")

	rotated, err := New(map[string][]byte{"v1": key(0x01), "v2": key(0x02)}, "v2")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if rotated.ActiveVersion() != "v2" {
		t.Errorf("ActiveVersion = %q, want v2", rotated.ActiveVersion())
	}
	got, err := rotated.Decrypt(envelope)
	if err != nil || got != "legacy" {
		t.Errorf("rotated.Decrypt = %q, %v; want legacy, nil", got, err)
	}
	// New ciphertexts seal under v2.
	fresh, _ := rotated.Encrypt("new")
	if !strings.HasPrefix(fresh, "v2:") {
		t.Errorf("new ciphertext %q should use active version v2", fresh)
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	if _, err := New(nil, "v1"); !errors.Is(err, ErrNoKeys) {
		t.Errorf("empty keys should return ErrNoKeys, got %v", err)
	}
	if _, err := New(map[string][]byte{"v1": key(0x01)}, "v2"); err == nil {
		t.Error("active version absent from keys should fail")
	}
	if _, err := New(map[string][]byte{"v1": make([]byte, 16)}, "v1"); err == nil {
		t.Error("non-32-byte key should fail")
	}
}

func TestNewFromEnv(t *testing.T) {
	k1 := base64.StdEncoding.EncodeToString(key(0x01))
	k2 := base64.StdEncoding.EncodeToString(key(0x02))
	t.Setenv(EnvKeys, "v1:"+k1+",v2:"+k2)
	t.Setenv(EnvActiveKeyVersion, "v2")

	c, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}
	if c.ActiveVersion() != "v2" {
		t.Errorf("ActiveVersion = %q, want v2", c.ActiveVersion())
	}
	envelope, _ := c.Encrypt("x")
	if got, _ := c.Decrypt(envelope); got != "x" {
		t.Errorf("round trip failed: %q", got)
	}
}

func TestNewFromEnvMissing(t *testing.T) {
	t.Setenv(EnvKeys, "")
	if _, err := NewFromEnv(); !errors.Is(err, ErrNoKeys) {
		t.Errorf("missing keys should return ErrNoKeys, got %v", err)
	}

	t.Setenv(EnvKeys, "v1:"+base64.StdEncoding.EncodeToString(key(0x01)))
	t.Setenv(EnvActiveKeyVersion, "")
	if _, err := NewFromEnv(); err == nil {
		t.Error("missing active version should fail")
	}
}
