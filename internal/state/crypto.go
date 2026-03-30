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

var (
	osHostname   = os.Hostname
	aesNewCipher = aes.NewCipher
	cipherNewGCM = cipher.NewGCM
	ioReadFull   = io.ReadFull
	randRead     = rand.Read
)

func getMachineKey() []byte {
	raw := getMachineID()
	// Derive a proper 32-byte AES key using SHA-256
	hash := sha256.Sum256([]byte("simpledeploy:" + raw))
	return hash[:]
}

func getMachineID() string {
	// Try /etc/machine-id (Linux)
	if data, err := osReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(data))
	}
	// Try /var/lib/dbus/machine-id (Linux)
	if data, err := osReadFile("/var/lib/dbus/machine-id"); err == nil {
		return strings.TrimSpace(string(data))
	}
	// Fallback: hostname + username
	hostname, _ := osHostname()
	user := os.Getenv("USER")
	if user == "" {
		user = "root"
	}
	return hostname + ":" + user + ":simpledeploy"
}

func Encrypt(plaintext string) (string, error) {
	key := getMachineKey()

	block, err := aesNewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipherNewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := ioReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func Decrypt(encoded string) (string, error) {
	key := getMachineKey()

	data, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode hex: %w", err)
	}

	block, err := aesNewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	aesGCM, err := cipherNewGCM(block)
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
	if _, err := randRead(bytes); err != nil {
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
		if _, err := randRead(batch); err != nil {
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
