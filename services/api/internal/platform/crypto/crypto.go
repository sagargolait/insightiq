// Package crypto seals data-source credentials with AES-256-GCM before they
// reach the database. Keys are supplied through the environment and carry a
// version so credentials sealed under an old key survive a key rotation: each
// ciphertext records the version that sealed it, and Decrypt selects the
// matching key.
//
// Nothing in this package logs plaintext, keys, or decrypted values.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// EnvKeys holds one or more versioned keys: "v1:<base64>,v2:<base64>".
	// Each key must decode to exactly 32 bytes (AES-256).
	EnvKeys = "DATA_ENCRYPTION_KEYS"
	// EnvActiveKeyVersion names the version new ciphertexts are sealed with.
	EnvActiveKeyVersion = "DATA_ENCRYPTION_ACTIVE_KEY_VERSION"

	keySize = 32
)

var (
	// ErrNoKeys means the environment supplied no usable encryption keys.
	ErrNoKeys = errors.New("no data encryption keys configured")
	// ErrUnknownKeyVersion means a ciphertext names a key version we do not hold.
	ErrUnknownKeyVersion = errors.New("unknown encryption key version")
	// ErrMalformedCiphertext means the envelope could not be parsed.
	ErrMalformedCiphertext = errors.New("malformed ciphertext")
)

// Cipher encrypts and decrypts credential strings. It is safe for concurrent
// use: the key material is immutable after construction.
type Cipher struct {
	keys          map[string][]byte // version -> 32-byte key
	activeVersion string
}

// New builds a Cipher from an explicit key set. keys maps a version label to a
// 32-byte key; activeVersion must be one of those labels and is used for
// sealing new ciphertexts. Mostly for tests; production uses NewFromEnv.
func New(keys map[string][]byte, activeVersion string) (*Cipher, error) {
	if len(keys) == 0 {
		return nil, ErrNoKeys
	}
	copied := make(map[string][]byte, len(keys))
	for version, key := range keys {
		if version == "" {
			return nil, fmt.Errorf("%w: empty key version", ErrNoKeys)
		}
		if len(key) != keySize {
			return nil, fmt.Errorf("key %q must be %d bytes, got %d", version, keySize, len(key))
		}
		dup := make([]byte, keySize)
		copy(dup, key)
		copied[version] = dup
	}
	if _, ok := copied[activeVersion]; !ok {
		return nil, fmt.Errorf("active key version %q is not among the configured keys", activeVersion)
	}
	return &Cipher{keys: copied, activeVersion: activeVersion}, nil
}

// NewFromEnv builds a Cipher from DATA_ENCRYPTION_KEYS and
// DATA_ENCRYPTION_ACTIVE_KEY_VERSION. Returns ErrNoKeys when the key env is
// empty so the caller can decide whether that is fatal.
func NewFromEnv() (*Cipher, error) {
	raw := strings.TrimSpace(os.Getenv(EnvKeys))
	if raw == "" {
		return nil, ErrNoKeys
	}

	keys := make(map[string][]byte)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		version, encoded, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf("%s entry %q must be <version>:<base64-key>", EnvKeys, entry)
		}
		version = strings.TrimSpace(version)
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf("decode key %q: %w", version, err)
		}
		keys[version] = key
	}

	active := strings.TrimSpace(os.Getenv(EnvActiveKeyVersion))
	if active == "" {
		return nil, fmt.Errorf("%s is required", EnvActiveKeyVersion)
	}

	return New(keys, active)
}

// ActiveVersion returns the version new ciphertexts are sealed with. Callers
// persist it alongside the ciphertext.
func (c *Cipher) ActiveVersion() string { return c.activeVersion }

func (c *Cipher) aead(version string) (cipher.AEAD, error) {
	key, ok := c.keys[version]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKeyVersion, version)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return gcm, nil
}

// Encrypt seals plaintext under the active key and returns a self-describing
// envelope "<version>:<base64(nonce||ciphertext)>". Decrypt reverses it.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	gcm, err := c.aead(c.activeVersion)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return c.activeVersion + ":" + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens an envelope produced by Encrypt. It fails closed: an unknown
// key version, tampered ciphertext, or wrong key all return an error and no
// plaintext.
func (c *Cipher) Decrypt(envelope string) (string, error) {
	version, encoded, ok := strings.Cut(envelope, ":")
	if !ok || version == "" || encoded == "" {
		return "", ErrMalformedCiphertext
	}

	gcm, err := c.aead(version)
	if err != nil {
		return "", err
	}

	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMalformedCiphertext, err)
	}
	if len(sealed) < gcm.NonceSize() {
		return "", ErrMalformedCiphertext
	}

	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// GCM authentication failed: tampering or wrong key. Do not leak detail.
		return "", fmt.Errorf("%w: authentication failed", ErrMalformedCiphertext)
	}
	return string(plaintext), nil
}
