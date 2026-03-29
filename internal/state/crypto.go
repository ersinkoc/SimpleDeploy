package state

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

func getMachineKey() ([]byte, error) {
	raw := getMachineID()
	// Derive a proper 32-byte AES key using SHA-256
	hash := sha256.Sum256([]byte("simpledeploy:" + raw))
	return hash[:], nil
}

func getMachineID() string {
	// Try /etc/machine-id (Linux)
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(data))
	}
	// Try /var/lib/dbus/machine-id (Linux)
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		return strings.TrimSpace(string(data))
	}
	// Fallback: hostname + username
	hostname, _ := os.Hostname()
	user := os.Getenv("USER")
	if user == "" {
		user = "root"
	}
	return hostname + ":" + user + ":simpledeploy"
}

func Encrypt(plaintext string) (string, error) {
	key, err := getMachineKey()
	if err != nil {
		return "", fmt.Errorf("failed to get encryption key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func Decrypt(encoded string) (string, error) {
	key, err := getMachineKey()
	if err != nil {
		return "", fmt.Errorf("failed to get decryption key: %w", err)
	}

	data, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode hex: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

func GenerateSecret(prefix string, length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate secret: %w", err)
	}
	return prefix + hex.EncodeToString(bytes)[:length], nil
}

func GeneratePassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	max := byte(256 - 256%len(charset)) // rejection sampling to avoid modulo bias
	i := 0
	for i < length {
		batch := make([]byte, length*2)
		if _, err := rand.Read(batch); err != nil {
			return "", fmt.Errorf("failed to generate password: %w", err)
		}
		for _, b := range batch {
			if b < max {
				result[i] = charset[int(b)%len(charset)]
				i++
				if i >= length {
					break
				}
			}
		}
	}
	return string(result), nil
}
