// Package crypto provides symmetric encryption for Cooker's
// secrets-at-rest. Values are sealed with AES-GCM using a 32-byte
// key. The key comes from COOKER_SECRET_KEY (base64, 32 bytes after
// decoding). Without a key the process refuses to handle secrets.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrNoKey is returned by Seal/Open when the codec has no key
// configured (the operator forgot to set COOKER_SECRET_KEY).
var ErrNoKey = errors.New("crypto: no secret key configured")

// Codec performs authenticated encryption with associated data (AEAD)
// using AES-GCM. Zero value is usable but refuses to seal/open.
type Codec struct {
	aead cipher.AEAD
}

// NewCodec parses a base64-encoded 32-byte key and returns a Codec.
// Empty keyB64 yields an inactive codec (Seal/Open return ErrNoKey);
// callers should treat that as a configuration signal, not an error.
func NewCodec(keyB64 string) (*Codec, error) {
	if keyB64 == "" {
		return &Codec{}, nil
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes after base64 decode, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &Codec{aead: gcm}, nil
}

// Active reports whether the codec can perform encryption.
func (c *Codec) Active() bool { return c != nil && c.aead != nil }

// Seal encrypts plaintext and returns (nonce ++ ciphertext ++ tag).
// The AEAD nonce is prepended so Open can recover it.
func (c *Codec) Seal(plaintext []byte) ([]byte, error) {
	if !c.Active() {
		return nil, ErrNoKey
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: random nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts a value produced by Seal.
func (c *Codec) Open(sealed []byte) ([]byte, error) {
	if !c.Active() {
		return nil, ErrNoKey
	}
	n := c.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce, ct := sealed[:n], sealed[n:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: open: %w", err)
	}
	return pt, nil
}

// GenerateKey returns a fresh 32-byte key encoded as base64. Useful
// for the `cooker generate-key` helper and for tests.
func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
