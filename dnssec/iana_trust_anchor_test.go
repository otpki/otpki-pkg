package dnssec

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/smallstep/pkcs7"
	"github.com/stretchr/testify/require"
)

func TestFetchIANARootTrustAnchors(t *testing.T) {
	now := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	publication := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<TrustAnchor id="test" source="https://example.test/root-anchors.xml">
  <Zone>.</Zone>
  <KeyDigest id="expired" validFrom="2010-01-01T00:00:00Z" validUntil="2020-01-01T00:00:00Z">
    <KeyTag>1</KeyTag><Algorithm>8</Algorithm><DigestType>2</DigestType><Digest>AA</Digest>
  </KeyDigest>
  <KeyDigest id="current" validFrom="2024-07-18T00:00:00Z">
    <KeyTag>38696</KeyTag><Algorithm>8</Algorithm><DigestType>2</DigestType>
    <Digest>683D 2D0A CB8C 9B71 2A19 48B2 7F74 1219 298D 0A45 0D61 2C48 3AF4 44A4 C0FB 2B16</Digest>
  </KeyDigest>
  <KeyDigest id="future" validFrom="2027-01-01T00:00:00Z">
    <KeyTag>2</KeyTag><Algorithm>8</Algorithm><DigestType>2</DigestType><Digest>BB</Digest>
  </KeyDigest>
</TrustAnchor>`)
	rootPEM, signature := signTestIANAPublication(t, publication, now)

	client := newIANATestClient(publication, signature)
	anchors, err := fetchIANARootTrustAnchors(
		t.Context(), client, "https://example.test/root-anchors.xml", "https://example.test/root-anchors.p7s", rootPEM, now,
	)
	require.NoError(t, err)
	require.Equal(t, []TrustAnchor{{
		Owner:      ".",
		KeyTag:     38696,
		Algorithm:  8,
		DigestType: 2,
		Digest:     "683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16",
	}}, anchors)
}

func TestFetchIANARootTrustAnchorsRejectsTamperedPublication(t *testing.T) {
	now := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	publication := []byte(validTestIANAPublication)
	rootPEM, signature := signTestIANAPublication(t, publication, now)
	publication = []byte(strings.ReplaceAll(string(publication), "20326", "38696"))

	client := newIANATestClient(publication, signature)
	_, err := fetchIANARootTrustAnchors(
		t.Context(), client, "https://example.test/root-anchors.xml", "https://example.test/root-anchors.p7s", rootPEM, now,
	)
	require.ErrorContains(t, err, "verify IANA root trust-anchor signature")
}

func TestParseIANARootTrustAnchorsRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	tests := map[string]string{
		"wrong zone":        strings.Replace(validTestIANAPublication, "<Zone>.</Zone>", "<Zone>example.</Zone>", 1),
		"no active anchors": strings.Replace(validTestIANAPublication, "2020-01-01", "2027-01-01", 1),
		"invalid digest":    strings.Replace(validTestIANAPublication, "E06D", "NOPE", 1),
		"trailing document": validTestIANAPublication + "<TrustAnchor/>",
	}
	for name, publication := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseIANARootTrustAnchors([]byte(publication), now)
			require.Error(t, err)
		})
	}
}

func TestParseIANARootTrustAnchorsVerifiesPublishedDNSKEY(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	publication := `<TrustAnchor id="test"><Zone>.</Zone>
  <KeyDigest id="Kmyv6jo" validFrom="2024-07-18T00:00:00Z">
    <KeyTag>38696</KeyTag><Algorithm>8</Algorithm><DigestType>2</DigestType>
    <Digest>683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16</Digest>
    <PublicKey>
      AwEAAa96jeuknZlaeSrvyAJj6ZHv28hhOKkx3rLGXVaC6rXTsDc449/cidlt
      pkyGwCJNnOAlFNKF2jBosZBU5eeHspaQWOmOElZsjICMQMC3aeHbGiShvZsx
      4wMYSjH8e7Vrhbu6irwCzVBApESjbUdpWWmEnhathWu1jo+siFUiRAAxm9qy
      JNg/wOZqqzL/dL/q8PkcRU5oUKEpUge71M3ej2/7CPqpdVwuMoTvoB+ZOT4
      YeGyxMvHmbrxlFzGOHOijtzN+u1TQNatX2XBuzZNQ1K+s2CXkPIZo7s6JgZ
      yvaBevYtxPvYLw4z9mR7K2vaF18UYH9Z9GNUUeayffKC73PYc=
    </PublicKey>
    <Flags>257</Flags>
  </KeyDigest>
</TrustAnchor>`
	anchors, err := parseIANARootTrustAnchors([]byte(publication), now)
	require.NoError(t, err)
	require.Len(t, anchors, 1)

	tampered := strings.Replace(publication, "683D2D0A", "E83D2D0A", 1)
	_, err = parseIANARootTrustAnchors([]byte(tampered), now)
	require.ErrorContains(t, err, "PublicKey does not match Digest")
}

func TestDownloadIANATrustAnchorFileRejectsUnexpectedResponses(t *testing.T) {
	t.Parallel()
	t.Run("status", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testHTTPResponse(http.StatusNotFound, nil), nil
		})}
		_, err := downloadIANATrustAnchorFile(t.Context(), client, "https://example.test")
		require.ErrorContains(t, err, "404 Not Found")
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testHTTPResponse(http.StatusOK, nil), nil
		})}
		_, err := downloadIANATrustAnchorFile(t.Context(), client, "https://example.test")
		require.ErrorContains(t, err, "response is empty")
	})
	t.Run("too large", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testHTTPResponse(http.StatusOK, make([]byte, maxIANATrustAnchorFileSize+1)), nil
		})}
		_, err := downloadIANATrustAnchorFile(t.Context(), client, "https://example.test")
		require.ErrorContains(t, err, "response exceeds")
	})
}

const validTestIANAPublication = `<TrustAnchor id="test" source="https://example.test/root-anchors.xml">
  <Zone>.</Zone>
  <KeyDigest id="current" validFrom="2020-01-01T00:00:00Z">
    <KeyTag>20326</KeyTag><Algorithm>8</Algorithm><DigestType>2</DigestType>
    <Digest>E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D</Digest>
  </KeyDigest>
</TrustAnchor>`

func newIANATestClient(publication, signature []byte) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/root-anchors.xml":
			return testHTTPResponse(http.StatusOK, publication), nil
		case "/root-anchors.p7s":
			return testHTTPResponse(http.StatusOK, signature), nil
		default:
			return testHTTPResponse(http.StatusNotFound, nil), nil
		}
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testHTTPResponse(statusCode int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}

func signTestIANAPublication(t *testing.T, publication []byte, now time.Time) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "IANA updater test root"},
		NotBefore:             now.AddDate(-1, 0, 0),
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(certificateDER)
	require.NoError(t, err)

	signedData, err := pkcs7.NewSignedData(publication)
	require.NoError(t, err)
	require.NoError(t, signedData.AddSigner(certificate, key, pkcs7.SignerInfoConfig{}))
	signedData.Detach()
	signature, err := signedData.Finish()
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}), signature
}
