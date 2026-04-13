package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/samber/oops"
	"golang.org/x/crypto/argon2"
)

// Argon2
const (
	saltLength = 16
	keyLength  = 32
	time       = 3
	memory     = 64 * 1024
	threads    = 4
)

// Argon password hashing
func HashPassword(password string) (string, error) {
	// Random salt
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", oops.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, time, memory, threads, keyLength)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, time, threads, b64Salt, b64Hash)

	return encoded, nil
}

// Compare string to password hash for auth
func VerifyPassword(password, encodedHash string) (bool, error) {
	if encodedHash == "" {
		return false, nil
	}

	// Split hash
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, oops.Errorf("invalid hash format")
	}

	if parts[1] != "argon2id" {
		return false, oops.Errorf("unsupported hash algorithm")
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return false, oops.Errorf("invalid version: %w", err)
	}

	if version != argon2.Version {
		return false, oops.Errorf("incompatible argon2 version")
	}

	var m, t, p uint32
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p)
	if err != nil {
		return false, oops.Errorf("invalid parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, oops.Errorf("invalid salt: %w", err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, oops.Errorf("invalid hash: %w", err)
	}

	// Get hash of string input
	hash := argon2.IDKey([]byte(password), salt, t, m, uint8(p), uint32(len(expectedHash)))

	// Compare against existing hash
	return subtle.ConstantTimeCompare(hash, expectedHash) == 1, nil
}
