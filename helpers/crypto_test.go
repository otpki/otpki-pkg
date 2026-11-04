package helpers

import (
	"crypto/ed25519"
	"crypto/rand"
	stdx509 "crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/smallstep/pkcs7"
	"github.com/stretchr/testify/require"
)

func testCertificateDER(t *testing.T) []byte {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	template := &stdx509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lint-regression.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := stdx509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	require.NoError(t, err)
	return der
}

func TestParseCertificateChainFromHeaderEncodings(t *testing.T) {
	der := testCertificateDER(t)
	pemCert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	for name, header := range map[string]string{
		"plain PEM":   pemCert,
		"escaped PEM": url.PathEscape(pemCert),
		"bare base64": base64.StdEncoding.EncodeToString(der),
	} {
		t.Run(name, func(t *testing.T) {
			certs, err := ParseCertificateChainFromHeaderValue(header)
			require.NoError(t, err)
			require.Len(t, certs, 1)
			require.Equal(t, der, certs[0].Raw)
		})
	}
}

func TestLoadCertificateChainFromP7bHasNoNilEntries(t *testing.T) {
	der := testCertificateDER(t)
	bundle, err := pkcs7.DegenerateCertificate(der)
	require.NoError(t, err)
	for name, data := range map[string][]byte{
		"DER": bundle,
		"PEM": pem.EncodeToMemory(&pem.Block{Type: "PKCS7", Bytes: bundle}),
	} {
		t.Run(name, func(t *testing.T) {
			certs, err := LoadCertificateChainFromP7b(data)
			require.NoError(t, err)
			require.Len(t, certs, 1)
			require.NotNil(t, certs[0])
			require.Equal(t, der, certs[0].Raw)
		})
	}
}
