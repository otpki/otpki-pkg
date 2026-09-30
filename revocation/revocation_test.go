package revocation

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"io"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/oids"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ocsp"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (fn roundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func testClient(t *testing.T, status int, response []byte, method string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, method, request.Method)
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(response)), Header: make(http.Header)}, nil
	})}
}

func testCertificate(t *testing.T, template, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	if parent == nil {
		parent, parentKey = template, key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, key
}

func TestOCSPStatus(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	issuer, issuerKey := testCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: "OCSP CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		SubjectKeyId: []byte{1, 2, 3},
	}, nil, nil)
	leaf, _ := testCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(101), Subject: pkix.Name{CommonName: "OCSP device"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage:   x509.KeyUsageDigitalSignature,
		OCSPServer: []string{"http://ocsp.example/status"},
	}, issuer, issuerKey)
	for _, test := range []struct {
		name        string
		status      int
		thisUpdate  time.Time
		nextUpdate  time.Time
		wantRevoked bool
		wantErr     bool
	}{
		{name: "good", status: ocsp.Good, thisUpdate: now.Add(-time.Minute), nextUpdate: now.Add(time.Hour)},
		{name: "revoked", status: ocsp.Revoked, thisUpdate: now.Add(-time.Minute), nextUpdate: now.Add(time.Hour), wantRevoked: true, wantErr: true},
		{name: "unknown", status: ocsp.Unknown, thisUpdate: now.Add(-time.Minute), nextUpdate: now.Add(time.Hour), wantErr: true},
		{name: "expired", status: ocsp.Good, thisUpdate: now.Add(-time.Hour), nextUpdate: now.Add(-time.Minute), wantErr: true},
		{name: "future", status: ocsp.Good, thisUpdate: now.Add(10 * time.Minute), nextUpdate: now.Add(time.Hour), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := ocsp.CreateResponse(issuer, issuer, ocsp.Response{
				Status: test.status, SerialNumber: leaf.SerialNumber,
				ThisUpdate: test.thisUpdate, NextUpdate: test.nextUpdate,
				RevokedAt: now.Add(-time.Hour),
			}, issuerKey)
			require.NoError(t, err)
			err = CheckChain(context.Background(), []*x509.Certificate{leaf, issuer}, Options{
				Client: testClient(t, http.StatusOK, response, http.MethodPost), Now: now,
			})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantRevoked, errors.Is(err, ErrRevoked))
		})
	}
}

func TestCRLStatus(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	issuer, issuerKey := testCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(200), Subject: pkix.Name{CommonName: "CRL CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		SubjectKeyId: []byte{4, 5, 6},
	}, nil, nil)
	leaf, _ := testCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(201), Subject: pkix.Name{CommonName: "CRL device"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		CRLDistributionPoints: []string{"http://crl.example/list"},
	}, issuer, issuerKey)
	for _, test := range []struct {
		name        string
		revoke      bool
		stale       bool
		delta       bool
		remove      bool
		wantRevoked bool
		wantErr     bool
	}{
		{name: "good"},
		{name: "revoked", revoke: true, wantRevoked: true, wantErr: true},
		{name: "stale", stale: true, wantErr: true},
		{name: "delta without entries", delta: true, wantErr: true},
		{name: "delta with removeFromCRL", delta: true, remove: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			nextUpdate := now.Add(time.Hour)
			if test.stale {
				nextUpdate = now.Add(-time.Minute)
			}
			list := &x509.RevocationList{Number: big.NewInt(1), ThisUpdate: now.Add(-time.Hour), NextUpdate: nextUpdate}
			if test.revoke {
				list.RevokedCertificateEntries = []x509.RevocationListEntry{{SerialNumber: leaf.SerialNumber, RevocationTime: now.Add(-time.Minute)}}
			}
			if test.delta {
				baseNumber, err := asn1.Marshal(1)
				require.NoError(t, err)
				list.Number = big.NewInt(2)
				list.ExtraExtensions = []pkix.Extension{{Id: oids.OidDeltaCRLIndicator, Critical: true, Value: baseNumber}}
			}
			if test.remove {
				list.RevokedCertificateEntries = []x509.RevocationListEntry{{SerialNumber: leaf.SerialNumber, RevocationTime: now.Add(-time.Minute), ReasonCode: 8}}
			}
			response, err := x509.CreateRevocationList(nil, list, issuer, issuerKey)
			require.NoError(t, err)
			err = CheckChain(context.Background(), []*x509.Certificate{leaf, issuer}, Options{
				Client: testClient(t, http.StatusOK, response, http.MethodGet), Now: now,
			})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if test.delta {
				require.ErrorContains(t, err, "delta CRL status is unsupported")
			}
			require.Equal(t, test.wantRevoked, errors.Is(err, ErrRevoked))
		})
	}
}

func TestCRLFallbackAndRevokedPrecedence(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	issuer, issuerKey := testCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(400), Subject: pkix.Name{CommonName: "Status CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		SubjectKeyId: []byte{7, 8, 9},
	}, nil, nil)
	leaf, _ := testCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(401), Subject: pkix.Name{CommonName: "device"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		OCSPServer:            []string{"http://ocsp.example/status"},
		CRLDistributionPoints: []string{"http://crl.example/list"},
	}, issuer, issuerKey)
	for _, test := range []struct {
		name        string
		ocspStatus  int
		crlRevoked  bool
		wantRevoked bool
	}{
		{name: "unknown OCSP falls back to good CRL", ocspStatus: ocsp.Unknown},
		{name: "revoked CRL overrides good OCSP", ocspStatus: ocsp.Good, crlRevoked: true, wantRevoked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ocspDER, err := ocsp.CreateResponse(issuer, issuer, ocsp.Response{
				Status: test.ocspStatus, SerialNumber: leaf.SerialNumber,
				ThisUpdate: now.Add(-time.Minute), NextUpdate: now.Add(time.Hour),
			}, issuerKey)
			require.NoError(t, err)
			list := &x509.RevocationList{Number: big.NewInt(1), ThisUpdate: now.Add(-time.Minute), NextUpdate: now.Add(time.Hour)}
			if test.crlRevoked {
				list.RevokedCertificateEntries = []x509.RevocationListEntry{{SerialNumber: leaf.SerialNumber, RevocationTime: now.Add(-time.Minute)}}
			}
			crlDER, err := x509.CreateRevocationList(nil, list, issuer, issuerKey)
			require.NoError(t, err)
			var ocspCalls, crlCalls int
			client := &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
				var data []byte
				switch request.Method {
				case http.MethodPost:
					ocspCalls++
					data = ocspDER
				case http.MethodGet:
					crlCalls++
					data = crlDER
				default:
					t.Fatalf("unexpected method %s", request.Method)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
			})}
			err = CheckChain(context.Background(), []*x509.Certificate{leaf, issuer}, Options{Client: client, Now: now})
			require.Equal(t, test.wantRevoked, errors.Is(err, ErrRevoked))
			if !test.wantRevoked {
				require.NoError(t, err)
			}
			require.Equal(t, 1, ocspCalls)
			require.Equal(t, 1, crlCalls)
		})
	}
}

func TestResponseBoundaries(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://localhost/status", "http://127.0.0.1/status", "file:///tmp/crl", "http://user:pass@example.com/status", "http://example.com:8080/status", "http://example.com/status#fragment"} {
		t.Run(raw, func(t *testing.T) {
			_, err := allowedURL(raw)
			if raw == "http://localhost/status" || raw == "http://127.0.0.1/status" {
				require.NoError(t, err) // Address filtering happens at dial time.
			} else {
				require.Error(t, err)
			}
		})
	}
	_, err := fetch(context.Background(), testClient(t, http.StatusOK,
		bytes.Repeat([]byte{'x'}, maxResponseBytes+1), http.MethodGet), http.MethodGet, "http://crl.example/list", "", nil)
	require.ErrorContains(t, err, "too large")
	for _, address := range []string{"127.0.0.1:80", "10.0.0.1:80", "169.254.1.1:80", "[::1]:80"} {
		_, err := dialPublicHost(context.Background(), "tcp", address)
		require.ErrorContains(t, err, "no permitted public address")
	}
}

func TestChainRequiresEveryNonAnchorStatus(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	issuer, issuerKey := testCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(300), Subject: pkix.Name{CommonName: "CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, nil, nil)
	leaf, _ := testCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(301), Subject: pkix.Name{CommonName: "device"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
	}, issuer, issuerKey)
	require.ErrorContains(t, CheckChain(context.Background(), []*x509.Certificate{leaf, issuer}, Options{Now: now}), "no OCSP or CRL location")
	require.ErrorContains(t, CheckChain(context.Background(), []*x509.Certificate{leaf}, Options{Now: now}), "incomplete")
}
