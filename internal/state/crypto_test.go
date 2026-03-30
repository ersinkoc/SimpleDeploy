package state

import (
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	tests := []string{
		"hello world",
		"ghp_1234567890abcdef",
		"",
		"password with spaces and !@#$%^&*()",
		"a",
		strings.Repeat("x", 1000),
	}

	for _, plaintext := range tests {
		t.Run("len="+string(rune(len(plaintext))), func(t *testing.T) {
			encrypted, err := Encrypt(plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			if encrypted == plaintext {
				t.Error("Encrypted should differ from plaintext")
			}

			decrypted, err := Decrypt(encrypted)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			if decrypted != plaintext {
				t.Errorf("Decrypt got %q, want %q", decrypted, plaintext)
			}
		})
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	enc1, _ := Encrypt("same")
	enc2, _ := Encrypt("same")
	if enc1 == enc2 {
		t.Error("Two encryptions of same plaintext should produce different ciphertexts (random nonce)")
	}
}

func TestDecryptInvalidHex(t *testing.T) {
	_, err := Decrypt("not-valid-hex!@#")
	if err == nil {
		t.Error("Should fail on invalid hex")
	}
}

func TestDecryptTooShort(t *testing.T) {
	_, err := Decrypt("ab")
	if err == nil {
		t.Error("Should fail on too-short ciphertext")
	}
}

func TestDecryptGarbage(t *testing.T) {
	_, err := Decrypt("aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344")
	if err == nil {
		t.Error("Should fail to decrypt garbage data")
	}
}

func TestGenerateSecret(t *testing.T) {
	secret, err := GenerateSecret("whk_", 32)
	if err != nil {
		t.Fatalf("GenerateSecret failed: %v", err)
	}
	if !strings.HasPrefix(secret, "whk_") {
		t.Error("Secret should have prefix")
	}
	if len(secret) != 4+32 {
		t.Errorf("Secret length = %d, want %d", len(secret), 36)
	}
}

func TestGenerateSecretUniqueness(t *testing.T) {
	s1, _ := GenerateSecret("test_", 16)
	s2, _ := GenerateSecret("test_", 16)
	if s1 == s2 {
		t.Error("Two generated secrets should be different")
	}
}

func TestGeneratePassword(t *testing.T) {
	pwd, err := GeneratePassword(20)
	if err != nil {
		t.Fatalf("GeneratePassword failed: %v", err)
	}
	if len(pwd) != 20 {
		t.Errorf("Password length = %d, want 20", len(pwd))
	}
}

func TestGeneratePasswordUniqueness(t *testing.T) {
	p1, _ := GeneratePassword(50)
	p2, _ := GeneratePassword(50)
	if p1 == p2 {
		t.Error("Two generated passwords should be different")
	}
}

func TestGeneratePasswordCharset(t *testing.T) {
	pwd, _ := GeneratePassword(1000)
	hasUpper, hasLower, hasDigit := false, false, false
	for _, c := range pwd {
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
		}
		if c >= 'a' && c <= 'z' {
			hasLower = true
		}
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		t.Error("Password should contain upper, lower, and digit chars")
	}
}

func TestGeneratePasswordLengths(t *testing.T) {
	for _, length := range []int{1, 5, 50, 256} {
		pwd, err := GeneratePassword(length)
		if err != nil {
			t.Fatalf("GeneratePassword(%d) failed: %v", length, err)
		}
		if len(pwd) != length {
			t.Errorf("GeneratePassword(%d) returned length %d", length, len(pwd))
		}
	}
}

func TestGenerateSecret_EmptyPrefix(t *testing.T) {
	secret, err := GenerateSecret("", 16)
	if err != nil {
		t.Fatalf("GenerateSecret failed: %v", err)
	}
	if len(secret) != 16 {
		t.Errorf("Secret length = %d, want 16", len(secret))
	}
}

func TestGenerateSecret_VariousLengths(t *testing.T) {
	for _, length := range []int{1, 8, 32, 64, 128} {
		secret, err := GenerateSecret("pfx_", length)
		if err != nil {
			t.Fatalf("GenerateSecret(%d) failed: %v", length, err)
		}
		if len(secret) != 4+length {
			t.Errorf("Secret length = %d, want %d", len(secret), 4+length)
		}
	}
}

func TestDecrypt_EmptyString(t *testing.T) {
	_, err := Decrypt("")
	if err == nil {
		t.Error("Should fail on empty string")
	}
}

func TestDecrypt_OddLengthHex(t *testing.T) {
	_, err := Decrypt("abc")
	if err == nil {
		t.Error("Should fail on odd-length hex")
	}
}

func TestEncryptDecrypt_SpecialChars(t *testing.T) {
	tests := []string{
		"hello\nworld",
		"tab\there",
		"quote\"test",
		"back\\slash",
		"null\x00byte",
	}
	for _, tc := range tests {
		enc, err := Encrypt(tc)
		if err != nil {
			t.Fatalf("Encrypt(%q) failed: %v", tc, err)
		}
		dec, err := Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt failed for %q: %v", tc, err)
		}
		if dec != tc {
			t.Errorf("Roundtrip failed for %q: got %q", tc, dec)
		}
	}
}

func TestGetMachineID(t *testing.T) {
	id := getMachineID()
	if id == "" {
		t.Error("getMachineID should not return empty")
	}
}

func TestGetMachineKey(t *testing.T) {
	key := getMachineKey()
	if len(key) != 32 {
		t.Errorf("Key length = %d, want 32 (AES-256)", len(key))
	}
}

func TestGetMachineID_HostnameError(t *testing.T) {
	oldHostname := osHostname
	osHostname = func() (string, error) {
		return "", errors.New("hostname error")
	}
	defer func() { osHostname = oldHostname }()

	id := getMachineID()
	if !strings.Contains(id, ":root:simpledeploy") {
		t.Errorf("Expected fallback with root user, got %q", id)
	}
}

func TestEncrypt_AesNewCipherError(t *testing.T) {
	old := aesNewCipher
	aesNewCipher = func(key []byte) (cipher.Block, error) {
		return nil, errors.New("cipher error")
	}
	defer func() { aesNewCipher = old }()

	_, err := Encrypt("test")
	if err == nil {
		t.Error("Encrypt should fail when aesNewCipher fails")
	}
}

func TestEncrypt_CipherNewGCMError(t *testing.T) {
	old := cipherNewGCM
	cipherNewGCM = func(block cipher.Block) (cipher.AEAD, error) {
		return nil, errors.New("gcm error")
	}
	defer func() { cipherNewGCM = old }()

	_, err := Encrypt("test")
	if err == nil {
		t.Error("Encrypt should fail when cipherNewGCM fails")
	}
}

func TestEncrypt_IoReadFullError(t *testing.T) {
	old := ioReadFull
	ioReadFull = func(r io.Reader, buf []byte) (int, error) {
		return 0, errors.New("nonce error")
	}
	defer func() { ioReadFull = old }()

	_, err := Encrypt("test")
	if err == nil {
		t.Error("Encrypt should fail when ioReadFull fails")
	}
}

func TestDecrypt_AesNewCipherError(t *testing.T) {
	enc, _ := Encrypt("test")
	old := aesNewCipher
	aesNewCipher = func(key []byte) (cipher.Block, error) {
		return nil, errors.New("cipher error")
	}
	defer func() { aesNewCipher = old }()

	_, err := Decrypt(enc)
	if err == nil {
		t.Error("Decrypt should fail when aesNewCipher fails")
	}
}

func TestDecrypt_CipherNewGCMError(t *testing.T) {
	enc, _ := Encrypt("test")
	old := cipherNewGCM
	cipherNewGCM = func(block cipher.Block) (cipher.AEAD, error) {
		return nil, errors.New("gcm error")
	}
	defer func() { cipherNewGCM = old }()

	_, err := Decrypt(enc)
	if err == nil {
		t.Error("Decrypt should fail when cipherNewGCM fails")
	}
}

func TestGenerateSecret_RandReadError(t *testing.T) {
	old := randRead
	randRead = func(b []byte) (int, error) {
		return 0, errors.New("rand error")
	}
	defer func() { randRead = old }()

	_, err := GenerateSecret("pre_", 16)
	if err == nil {
		t.Error("GenerateSecret should fail when randRead fails")
	}
}

func TestGetMachineID_EtcMachineID(t *testing.T) {
	oldReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		if name == "/etc/machine-id" {
			return []byte("abc123\n"), nil
		}
		return nil, os.ErrNotExist
	}
	defer func() { osReadFile = oldReadFile }()

	id := getMachineID()
	if id != "abc123" {
		t.Errorf("getMachineID() = %q, want 'abc123'", id)
	}
}

func TestGetMachineID_DBusMachineID(t *testing.T) {
	oldReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		if name == "/etc/machine-id" {
			return nil, os.ErrNotExist
		}
		if name == "/var/lib/dbus/machine-id" {
			return []byte("dbus456\n"), nil
		}
		return nil, os.ErrNotExist
	}
	defer func() { osReadFile = oldReadFile }()

	id := getMachineID()
	if id != "dbus456" {
		t.Errorf("getMachineID() = %q, want 'dbus456'", id)
	}
}

func TestGeneratePassword_RandReadError(t *testing.T) {
	old := randRead
	calls := 0
	randRead = func(b []byte) (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("rand error")
		}
		return rand.Read(b)
	}
	defer func() { randRead = old }()

	_, err := GeneratePassword(16)
	if err == nil {
		t.Error("GeneratePassword should fail when randRead fails")
	}
}
