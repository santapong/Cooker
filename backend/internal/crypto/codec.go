// Package crypto provides symmetric encryption for Cooker's
// secrets-at-rest. Values are sealed with AES-GCM using a 32-byte
// key. The key comes from COOKER_SECRET_KEY (base64, 32 bytes after
// decoding). Without a key the process refuses to handle secrets.
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	aead            cipher.AEAD
	keyID           [16]byte
	manifestKey     [32]byte
	versionedWrites bool
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
	c := &Codec{aead: gcm}
	id := hmac.New(sha256.New, key)
	id.Write([]byte("cooker:secret-key-id:v1"))
	copy(c.keyID[:], id.Sum(nil))
	m := hmac.New(sha256.New, key)
	m.Write([]byte("cooker:rotation-manifest:v1"))
	copy(c.manifestKey[:], m.Sum(nil))
	clear(key)
	return c, nil
}

// Active reports whether the codec can perform encryption.
func (c *Codec) Active() bool { return c != nil && c.aead != nil }

// Seal encrypts plaintext and returns (nonce ++ ciphertext ++ tag).
// The AEAD nonce is prepended so Open can recover it.
func (c *Codec) Seal(plaintext []byte) ([]byte, error) {
	if !c.Active() {
		return nil, ErrNoKey
	}
	if c.versionedWrites {
		return c.SealVersioned(plaintext)
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
	if bytes.HasPrefix(sealed, envelopeMagic) {
		headerLen := len(envelopeMagic) + 1 + len(c.keyID)
		if len(sealed) < headerLen+c.aead.NonceSize()+c.aead.Overhead() || sealed[len(envelopeMagic)] != 1 {
			return nil, errors.New("crypto: invalid versioned ciphertext")
		}
		if !hmac.Equal(sealed[len(envelopeMagic)+1:headerLen], c.keyID[:]) {
			return nil, errors.New("crypto: ciphertext key version mismatch")
		}
		n := c.aead.NonceSize()
		plain, err := c.aead.Open(nil, sealed[headerLen:headerLen+n], sealed[headerLen+n:], sealed[:headerLen])
		if err != nil {
			return nil, errors.New("crypto: ciphertext authentication failed")
		}
		return plain, nil
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

// A long prefix makes collision with a legacy random nonce negligible. Tagged
// records authenticate both their format and key identifier as AEAD data.
var envelopeMagic = []byte{0x43, 0x4b, 0x52, 0x53, 0x45, 0x41, 0x4c, 0x45, 0x44, 0x00, 0x9e, 0x73, 0x52, 0xbd, 0x21, 0x08}

// WithVersionedWrites returns a copy that emits tagged-v1 ciphertext. Readers
// accept tagged and legacy values; default writes remain legacy for staged upgrades.
func (c *Codec) WithVersionedWrites() *Codec {
	if c == nil {
		return nil
	}
	copy := *c
	copy.versionedWrites = true
	return &copy
}

// KeyID is a domain-separated identifier, never the encryption key.
func (c *Codec) KeyID() string {
	if !c.Active() {
		return ""
	}
	return hex.EncodeToString(c.keyID[:])
}

// SealVersioned emits an authenticated key-version header without changing the
// logical configuration generation. Only the offline rotation tool uses this
// directly; normal writers enable it through explicit server configuration.
func (c *Codec) SealVersioned(plaintext []byte) ([]byte, error) {
	if !c.Active() {
		return nil, ErrNoKey
	}
	header := append([]byte(nil), envelopeMagic...)
	header = append(header, 1)
	header = append(header, c.keyID[:]...)
	n := c.aead.NonceSize()
	out := make([]byte, len(header)+n)
	copy(out, header)
	if _, err := io.ReadFull(rand.Reader, out[len(header):]); err != nil {
		return nil, errors.New("crypto: random nonce unavailable")
	}
	return c.aead.Seal(out, out[len(header):], plaintext, header), nil
}

// RotationManifestMAC authenticates an offline backup's identity, record
// bindings and ciphertext under a separate domain from value encryption.
func (c *Codec) RotationManifestMAC(data []byte) []byte {
	if !c.Active() {
		return nil
	}
	m := hmac.New(sha256.New, c.manifestKey[:])
	m.Write(data)
	return m.Sum(nil)
}
