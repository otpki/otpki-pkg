package cryptoenvelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"

	"github.com/samber/oops"
)

const (
	ServiceName                       = "cryptoenvelope"
	encryptedDataEnvelopeVersion byte = 1
	encryptedDataHeaderSize           = 2 // version + nonce length
)

var ErrInvalidEncryptedDataEnvelope = oops.New("invalid encrypted data envelope")

// EncryptWithDEK encrypts plaintext with AES-GCM and encodes
// version + nonce + ciphertext into a compact binary envelope.
func EncryptWithDEK(plaintext, dek []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "creating AES cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "creating GCM")
	}
	nonceSize := gcm.NonceSize()
	if nonceSize < 0 || nonceSize > 255 {
		return nil, oops.In(ServiceName).New("GCM nonce length exceeds envelope encoding")
	}
	nonce := make([]byte, nonceSize)
	if _, err = rand.Read(nonce); err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "generating GCM nonce")
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, encryptedDataHeaderSize+len(nonce)+len(ciphertext))
	out[0] = encryptedDataEnvelopeVersion
	out[1] = byte(nonceSize)
	copy(out[encryptedDataHeaderSize:], nonce)
	copy(out[encryptedDataHeaderSize+len(nonce):], ciphertext)
	return out, nil
}

// DecryptWithDEK decrypts a binary envelope produced by EncryptWithDEK.
func DecryptWithDEK(envelopedCiphertext, dek []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "creating AES cipher")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "creating GCM")
	}
	if len(envelopedCiphertext) < encryptedDataHeaderSize {
		return nil, oops.In(ServiceName).Wrapf(ErrInvalidEncryptedDataEnvelope, "encrypted key data envelope is too short")
	}

	version := envelopedCiphertext[0]
	if version != encryptedDataEnvelopeVersion {
		return nil, oops.In(ServiceName).Wrapf(ErrInvalidEncryptedDataEnvelope, "unsupported encrypted key data envelope version %d", version)
	}
	nonceLen := int(envelopedCiphertext[1])
	if nonceLen != gcm.NonceSize() {
		return nil, oops.In(ServiceName).Wrapf(ErrInvalidEncryptedDataEnvelope, "invalid encrypted key data nonce length %d, expected %d", nonceLen, gcm.NonceSize())
	}
	if len(envelopedCiphertext) < encryptedDataHeaderSize+nonceLen {
		return nil, oops.In(ServiceName).Wrapf(ErrInvalidEncryptedDataEnvelope, "encrypted key data envelope has truncated nonce")
	}

	nonce := envelopedCiphertext[encryptedDataHeaderSize : encryptedDataHeaderSize+nonceLen]
	ciphertext := envelopedCiphertext[encryptedDataHeaderSize+nonceLen:]
	if len(ciphertext) == 0 {
		return nil, oops.In(ServiceName).Wrapf(ErrInvalidEncryptedDataEnvelope, "encrypted key data envelope has empty ciphertext")
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "decrypting key storage key config envelope")
	}
	return plaintext, nil
}
