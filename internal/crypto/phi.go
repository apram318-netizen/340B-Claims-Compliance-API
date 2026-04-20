// Package crypto provides application-layer encryption helpers for PHI fields.
// Fields that need equality-matching across calls (e.g. hashed_rx_key used for
// deduplication) use HMAC-SHA256 (deterministic keyed hash).
// Fields that are purely for display and never queried use AES-256-GCM
// (non-deterministic, semantically secure).
//
// Key management:
//   PHI_ENCRYPTION_KEY must be a 32-byte value base64-encoded as a standard
//   (padded) base64 string, e.g.:
//     openssl rand -base64 32
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrKeyNotSet is returned when PHI_ENCRYPTION_KEY is not configured.
var ErrKeyNotSet = errors.New("PHI_ENCRYPTION_KEY is not set")

// IsEnabled reports whether the encryption key is present in the environment.
func IsEnabled() bool {
	return os.Getenv("PHI_ENCRYPTION_KEY") != ""
}

// keyFromEnv reads and validates the 32-byte AES key from the environment.
func keyFromEnv() ([]byte, error) {
	k := os.Getenv("PHI_ENCRYPTION_KEY")
	if k == "" {
		return nil, ErrKeyNotSet
	}
	b, err := base64.StdEncoding.DecodeString(k)
	if err != nil {
		return nil, fmt.Errorf("decode PHI_ENCRYPTION_KEY: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("PHI_ENCRYPTION_KEY must be exactly 32 bytes after base64 decode, got %d", len(b))
	}
	return b, nil
}

// Encrypt encrypts plaintext with AES-256-GCM.
// The returned string is base64(nonce || ciphertext || tag).
// Use for fields stored at rest that are never compared for equality (non-searchable).
func Encrypt(plaintext string) (string, error) {
	key, err := keyFromEnv()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt decrypts a value produced by Encrypt.
func Decrypt(ciphertext string) (string, error) {
	key, err := keyFromEnv()
	if err != nil {
		return "", err
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ct) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, body := ct[:gcm.NonceSize()], ct[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(pt), nil
}

// HMAC returns a keyed HMAC-SHA256 hex string for searchable encrypted fields.
// The same input always produces the same output (deterministic), making it safe
// to use for equality comparisons (e.g. matching hashed_rx_key across claims and
// rebate records).
func HMAC(data string) (string, error) {
	key, err := keyFromEnv()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return fmt.Sprintf("%x", mac.Sum(nil)), nil
}
