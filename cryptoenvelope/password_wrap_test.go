package cryptoenvelope_test

import (
	"bytes"
	"testing"

	"github.com/otpki/otpki-pkg/cryptoenvelope"
	"github.com/stretchr/testify/require"
)

func TestWrapUnwrapDEKWithPassword_RoundTrip(t *testing.T) {
	t.Parallel()
	password := "correct horse battery staple"
	dek := bytes.Repeat([]byte{0x5A}, 32)

	wrapped, err := cryptoenvelope.WrapDEKWithPassword(password, dek)
	require.NoError(t, err)
	require.NotEmpty(t, wrapped)

	unwrapped, err := cryptoenvelope.UnwrapDEKWithPassword(password, wrapped)
	require.NoError(t, err)
	require.Equal(t, dek, unwrapped)
}

func TestWrapDEKWithPassword_UniqueOutput(t *testing.T) {
	t.Parallel()
	password := "correct horse battery staple"
	dek := bytes.Repeat([]byte{0x5A}, 32)

	wrappedA, err := cryptoenvelope.WrapDEKWithPassword(password, dek)
	require.NoError(t, err)
	wrappedB, err := cryptoenvelope.WrapDEKWithPassword(password, dek)
	require.NoError(t, err)

	require.NotEqual(t, wrappedA, wrappedB)
}

func TestUnwrapDEKWithPassword_WrongPasswordFails(t *testing.T) {
	t.Parallel()
	dek := bytes.Repeat([]byte{0xA5}, 32)
	wrapped, err := cryptoenvelope.WrapDEKWithPassword("password-a", dek)
	require.NoError(t, err)

	_, err = cryptoenvelope.UnwrapDEKWithPassword("password-b", wrapped)
	require.Error(t, err)
}
