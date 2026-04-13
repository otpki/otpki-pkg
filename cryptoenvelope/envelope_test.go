package cryptoenvelope_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otpki/otpki-pkg/cryptoenvelope"
	"github.com/stretchr/testify/require"
)

func TestEncryptWithDEK_UniqueNonceAndRoundTrip(t *testing.T) {
	t.Parallel()
	dek := bytes.Repeat([]byte{0x42}, 32)
	plaintext := []byte("test-private-key-config")

	encA, err := cryptoenvelope.EncryptWithDEK(plaintext, dek)
	require.NoError(t, err)
	encB, err := cryptoenvelope.EncryptWithDEK(plaintext, dek)
	require.NoError(t, err)

	require.NotEqual(t, encA, encB, "ciphertext envelope should differ because nonce must be unique")

	decA, err := cryptoenvelope.DecryptWithDEK(encA, dek)
	require.NoError(t, err)
	require.Equal(t, plaintext, decA)

	decB, err := cryptoenvelope.DecryptWithDEK(encB, dek)
	require.NoError(t, err)
	require.Equal(t, plaintext, decB)
}

func TestDecryptWithDEK_NonEnvelopeCiphertextFails(t *testing.T) {
	t.Parallel()
	dek := bytes.Repeat([]byte{0x24}, 32)
	nonEnvelopeCiphertext := []byte("legacy-private-key-config")

	_, err := cryptoenvelope.DecryptWithDEK(nonEnvelopeCiphertext, dek)
	require.Error(t, err)
	require.True(t, errors.Is(err, cryptoenvelope.ErrInvalidEncryptedDataEnvelope))
}
