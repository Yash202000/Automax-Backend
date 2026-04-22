package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

// EncryptLicenseKey encrypts plaintext using AES-256-GCM.
// hexKey must be a 64-character hex string (32 bytes).
// Returns base64-encoded ciphertext and base64-encoded nonce.
func EncryptLicenseKey(plaintext string, hexKey string) (ciphertext string, nonce string, err error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", "", fmt.Errorf("invalid encryption key hex: %w", err)
	}
	if len(key) != 32 {
		return "", "", fmt.Errorf("encryption key must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceBytes := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	encrypted := aesGCM.Seal(nil, nonceBytes, []byte(plaintext), nil)

	return base64.StdEncoding.EncodeToString(encrypted),
		base64.StdEncoding.EncodeToString(nonceBytes),
		nil
}

// DecryptLicenseKey decrypts base64-encoded ciphertext using AES-256-GCM.
// hexKey must be a 64-character hex string (32 bytes).
// nonce must be base64-encoded.
func DecryptLicenseKey(ciphertextB64 string, nonceB64 string, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("invalid encryption key hex: %w", err)
	}
	if len(key) != 32 {
		return "", fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	ciphertextBytes, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext base64: %w", err)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return "", fmt.Errorf("invalid nonce base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := aesGCM.Open(nil, nonceBytes, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed (wrong key or corrupted data): %w", err)
	}

	return string(plaintext), nil
}
