package helpers_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	stdX509 "crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/otpki/iogo/crypto/x509"

	"github.com/stretchr/testify/require"
	"go.step.sm/crypto/pemutil"
	"golang.org/x/crypto/ssh"

	"github.com/otpki/otpki-pkg/helpers"
)

func TestNormalizePrivateKeyToPKCS8PEM_UnencryptedPKCS8(t *testing.T) {
	t.Parallel()

	privateKey := generateRSAKey(t)
	privateKeyPEM, err := helpers.MarshalPrivateKey(privateKey)
	require.NoError(t, err)

	assertParsedAndNormalizedRSAKey(t, privateKeyPEM, "")
}

func TestNormalizePrivateKeyToPKCS8PEM_EncryptedPKCS8(t *testing.T) {
	t.Parallel()

	privateKey := generateRSAKey(t)
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)

	encryptedBlock, err := pemutil.EncryptPKCS8PrivateKey(
		rand.Reader,
		privateKeyDER,
		[]byte("secret"),
		stdX509.PEMCipherAES256,
	)
	require.NoError(t, err)

	privateKeyPEM := pem.EncodeToMemory(encryptedBlock)
	assertParsedAndNormalizedRSAKey(t, privateKeyPEM, "secret")
}

func TestNormalizePrivateKeyToPKCS8PEM_UnencryptedPKCS1(t *testing.T) {
	t.Parallel()

	privateKey := generateRSAKey(t)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	assertParsedAndNormalizedRSAKey(t, privateKeyPEM, "")
}

func TestNormalizePrivateKeyToPKCS8PEM_EncryptedPKCS1(t *testing.T) {
	t.Parallel()

	privateKey := generateRSAKey(t)
	encryptedBlock, err := x509.EncryptPEMBlock(
		rand.Reader,
		"RSA PRIVATE KEY",
		x509.MarshalPKCS1PrivateKey(privateKey),
		[]byte("secret"),
		x509.PEMCipherAES256,
	)
	require.NoError(t, err)

	privateKeyPEM := pem.EncodeToMemory(encryptedBlock)
	assertParsedAndNormalizedRSAKey(t, privateKeyPEM, "secret")
}

func TestNormalizePrivateKeyToPKCS8PEM_UnencryptedOpenSSH(t *testing.T) {
	t.Parallel()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	privateKeyBlock, err := ssh.MarshalPrivateKey(privateKey, "otpki-test")
	require.NoError(t, err)

	privateKeyPEM := pem.EncodeToMemory(privateKeyBlock)
	parsedKey, normalized, err := helpers.ParseAndNormalizePrivateKeyToPKCS8PEM(privateKeyPEM, "")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(normalized), "-----BEGIN PRIVATE KEY-----"))

	_, ok := parsedKey.(ed25519.PrivateKey)
	require.True(t, ok)
}

func TestNormalizePrivateKeyToPKCS8PEM_EncryptedOpenSSH(t *testing.T) {
	t.Parallel()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	privateKeyBlock, err := ssh.MarshalPrivateKeyWithPassphrase(
		privateKey,
		"otpki-test",
		[]byte("secret"),
	)
	require.NoError(t, err)

	privateKeyPEM := pem.EncodeToMemory(privateKeyBlock)
	parsedKey, normalized, err := helpers.ParseAndNormalizePrivateKeyToPKCS8PEM(privateKeyPEM, "secret")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(normalized), "-----BEGIN PRIVATE KEY-----"))

	_, ok := parsedKey.(ed25519.PrivateKey)
	require.True(t, ok)
}

func TestNormalizePrivateKeyToPKCS8PEM_EncryptedKeyWrongPassword(t *testing.T) {
	t.Parallel()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	privateKeyBlock, err := ssh.MarshalPrivateKeyWithPassphrase(
		privateKey,
		"otpki-test",
		[]byte("secret"),
	)
	require.NoError(t, err)

	privateKeyPEM := pem.EncodeToMemory(privateKeyBlock)
	_, _, err = helpers.ParseAndNormalizePrivateKeyToPKCS8PEM(privateKeyPEM, "wrong")
	require.Error(t, err)
}

func generateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return privateKey
}

func assertNormalizedRSAKey(t *testing.T, privateKeyPEM []byte, password string) {
	t.Helper()

	_, normalized, err := helpers.ParseAndNormalizePrivateKeyToPKCS8PEM(privateKeyPEM, password)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(normalized), "-----BEGIN PRIVATE KEY-----"))

	parsedKey, err := helpers.UnmarshalPrivateKey(normalized)
	require.NoError(t, err)
	_, ok := parsedKey.(*rsa.PrivateKey)
	require.True(t, ok)
}

func assertParsedAndNormalizedRSAKey(t *testing.T, privateKeyPEM []byte, password string) {
	t.Helper()

	parsedKey, normalized, err := helpers.ParseAndNormalizePrivateKeyToPKCS8PEM(privateKeyPEM, password)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(normalized), "-----BEGIN PRIVATE KEY-----"))

	_, ok := parsedKey.(*rsa.PrivateKey)
	require.True(t, ok)

	assertNormalizedRSAKey(t, privateKeyPEM, password)
}
