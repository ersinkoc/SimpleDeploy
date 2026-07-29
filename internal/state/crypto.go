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
	// Guard the arithmetic below: a non-positive length would reach
	// make([]byte, negative) and panic, and a zero-length secret is never
	// something a caller legitimately wants.
	if length <= 0 {
		return "", fmt.Errorf("secret length must be positive, got %d", length)
	}
	byteLen := (length + 1) / 2
	b := make([]byte, byteLen)
	if _, err := randRead(b); err != nil {
		return "", fmt.Errorf("failed to generate secret: %w", err)
	}
	return prefix + hex.EncodeToString(b)[:length], nil
}

func GeneratePassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	if length <= 0 {
		return "", fmt.Errorf("password length must be positive, got %d", length)
	}

	n := len(charset)
	// Rejection sampling. Only byte values below the largest multiple of n that
	// fits in a byte (248 for this 62-character set) are used; taking b%n over
	// the full 0-255 range would make the first 256%n characters measurably
	// more likely than the rest.
	//
	// The threshold is compared as an int rather than converted to a byte. The
	// previous `byte(256 - 256%len(charset))` was correct for this specific
	// charset but is a narrowing conversion whose safety depends on a value the
	// compiler cannot check — it silently wraps for any charset of length 1,
	// and static analysis flags it as a potential integer overflow.
	limit := 256 - (256 % n)

	result := make([]byte, length)
	for i := 0; i < length; {
		batch := make([]byte, length*2)
		if _, err := randRead(batch); err != nil {
			return "", fmt.Errorf("failed to generate password: %w", err)
		}
		for _, b := range batch {
			if int(b) >= limit {
				continue // biased tail of the byte range — discard and retry
			}
			result[i] = charset[int(b)%n]
			i++
			if i >= length {
				break
			}
		}
	}
	return string(result), nil
}
