package secretform

import (
	"crypto/aes"
	"crypto/sha512"
	"encoding/hex"

	"github.com/samber/oops"
	"golang.org/x/crypto/pbkdf2"
)

func GetKCVFromPassword(password string, salt []byte, iterations int, kcvLen int) (string, error) {
	key := pbkdf2.Key([]byte(password), salt, iterations, 32, sha512.New)
	return GetKCVFromKey(key, kcvLen)
}

func GetKCVFromKey(key []byte, kcvLen int) (string, error) {
	if len(key) != 32 {
		return "", oops.Errorf("invalid AES-256 key length: %d", len(key))
	}
	if kcvLen < 1 {
		return "", oops.Errorf("kcvLen must be >= 1")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	plain := make([]byte, aes.BlockSize)
	ciphertext := make([]byte, aes.BlockSize)
	block.Encrypt(ciphertext, plain)
	hexStr := hex.EncodeToString(ciphertext)
	if kcvLen > len(hexStr) {
		return "", oops.Errorf("kcvLen=%d exceeds available hex length=%d", kcvLen, len(hexStr))
	}
	return hexStr[:kcvLen], nil
}
