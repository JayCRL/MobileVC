package planning

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
)

// DeriveKey creates a 32-byte AES key from a secret string.
func DeriveKey(secret string) []byte {
	h := sha256.Sum256([]byte("mobilevc-planning:" + secret))
	return h[:]
}

// Encrypt encrypts plaintext using AES-256-GCM with the given key.
// Returns base64-encoded ciphertext.
func Encrypt(plaintext string, secret string) (string, error) {
	key := DeriveKey(secret)
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
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded ciphertext using AES-256-GCM.
func Decrypt(cipherB64 string, secret string) (string, error) {
	key := DeriveKey(secret)
	ciphertext, err := base64.StdEncoding.DecodeString(cipherB64)
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
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// ID returns a short fingerprint of the key for display (first 4 + last 4 chars).
func KeyHint(key string) string {
	if len(key) < 12 {
		return key[:4] + "..."
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// GenerateRandomHex returns a random hex string of n bytes (2*n chars).
func GenerateRandomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
