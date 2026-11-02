package cryptoenvelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"os"

	"github.com/samber/oops"
	"golang.org/x/crypto/argon2"
)

const (
	KeyEncryptionPasswordEnv = "OTPKI_KEY_ENCRYPTION_PASSWORD" //nolint:gosec // Environment variable name, not a credential.

	passwordWrapSaltLen   = 16
	passwordWrapKeyLen    = 32
	passwordWrapHeaderLen = 11 // salt_len + nonce_len + parallelism + iterations + memory_kib

	argon2IDMemoryKiB   = 64 * 1024
	argon2IDIterations  = 3
	argon2IDParallelism = 1
)

func PasswordWrapSecretFromEnv() (string, error) {
	secret := os.Getenv(KeyEncryptionPasswordEnv)
	if secret == "" {
		return "", oops.In(ServiceName).Errorf("%s is required for crypto_key_id=0", KeyEncryptionPasswordEnv)
	}
	return secret, nil
}

func IsEnvironmentTokenConfigured() bool {
	return os.Getenv(KeyEncryptionPasswordEnv) != ""
}

func WrapDEKWithPassword(password string, dek []byte) ([]byte, error) {
	if len(dek) == 0 {
		return nil, oops.In(ServiceName).New("DEK is empty")
	}
	salt := make([]byte, passwordWrapSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "generating Argon2id salt")
	}
	kek := argon2.IDKey(
		[]byte(password),
		salt,
		argon2IDIterations,
		argon2IDMemoryKiB,
		argon2IDParallelism,
		passwordWrapKeyLen,
	)

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "creating KEK cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "creating KEK GCM")
	}
	nonceSize := gcm.NonceSize()
	if nonceSize < 0 || nonceSize > 255 {
		return nil, oops.In(ServiceName).New("GCM nonce length exceeds envelope encoding")
	}
	nonce := make([]byte, nonceSize)
	if _, err = rand.Read(nonce); err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "generating DEK wrap nonce")
	}
	wrappedDEK := gcm.Seal(nil, nonce, dek, nil)

	out := make([]byte, passwordWrapHeaderLen+len(salt)+len(nonce)+len(wrappedDEK))
	out[0] = passwordWrapSaltLen
	out[1] = byte(nonceSize)
	out[2] = byte(argon2IDParallelism)
	binary.BigEndian.PutUint32(out[3:7], uint32(argon2IDIterations))
	binary.BigEndian.PutUint32(out[7:11], uint32(argon2IDMemoryKiB))
	copy(out[passwordWrapHeaderLen:], salt)
	copy(out[passwordWrapHeaderLen+len(salt):], nonce)
	copy(out[passwordWrapHeaderLen+len(salt)+len(nonce):], wrappedDEK)
	return out, nil
}

func UnwrapDEKWithPassword(password string, wrappedEnvelope []byte) ([]byte, error) {
	if len(wrappedEnvelope) < passwordWrapHeaderLen {
		return nil, oops.In(ServiceName).New("password-wrapped DEK envelope is too short")
	}
	saltLen := int(wrappedEnvelope[0])
	nonceLen := int(wrappedEnvelope[1])
	parallelism := wrappedEnvelope[2]
	iterations := binary.BigEndian.Uint32(wrappedEnvelope[3:7])
	memoryKiB := binary.BigEndian.Uint32(wrappedEnvelope[7:11])

	if saltLen == 0 || nonceLen == 0 {
		return nil, oops.In(ServiceName).New("password-wrapped DEK envelope has invalid salt/nonce length")
	}
	if parallelism == 0 || iterations == 0 || memoryKiB == 0 {
		return nil, oops.In(ServiceName).New("password-wrapped DEK envelope has invalid Argon2id params")
	}

	saltOffset := passwordWrapHeaderLen
	nonceOffset := saltOffset + saltLen
	ciphertextOffset := nonceOffset + nonceLen
	if len(wrappedEnvelope) <= ciphertextOffset {
		return nil, oops.In(ServiceName).New("password-wrapped DEK envelope is truncated")
	}

	salt := wrappedEnvelope[saltOffset:nonceOffset]
	nonce := wrappedEnvelope[nonceOffset:ciphertextOffset]
	ciphertext := wrappedEnvelope[ciphertextOffset:]

	kek := argon2.IDKey(
		[]byte(password),
		salt,
		iterations,
		memoryKiB,
		parallelism,
		passwordWrapKeyLen,
	)

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "creating KEK cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "creating KEK GCM")
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, oops.In(ServiceName).Errorf("invalid DEK wrap nonce length %d, expected %d", len(nonce), gcm.NonceSize())
	}
	dek, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "unwrapping DEK with password")
	}
	return dek, nil
}
