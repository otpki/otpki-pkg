// Package revocation checks certificate status using OCSP and CRLs advertised
// by a verified certificate chain. Trust path validation is the caller's job.
package revocation

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/otpki/otpki-pkg/oids"
	"golang.org/x/crypto/ocsp"
)

const maxResponseBytes = 1 << 20

// ErrRevoked means a signed, current OCSP response or complete CRL lists the
// certificate as revoked. Callers must not treat this as an unavailable status.
var ErrRevoked = errors.New("certificate is revoked")

// Options controls status checking. A nil Client uses a public-network-only
// client. Callers providing a client are responsible for its network policy;
// responder URLs are supplied by certificates and are not inherently trusted.
type Options struct {
	Client *http.Client
	Now    time.Time
}

var publicClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           dialPublicHost,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
		DisableKeepAlives:     true,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("revocation responder redirects are not allowed")
	},
}

// CheckChain checks every non-anchor certificate in a verified chain. Revoked
// status is conclusive. If neither OCSP nor a complete CRL establishes fresh
// status for any certificate, it returns an error for the caller's policy to
// handle. A nil client uses the package's public-network-only HTTP client.
func CheckChain(ctx context.Context, chain []*x509.Certificate, options Options) error {
	if len(chain) < 2 {
		return fmt.Errorf("verified certificate path is incomplete")
	}
	client := options.Client
	if client == nil {
		client = publicClient
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	var unavailable error
	for i := 0; i < len(chain)-1; i++ {
		if chain[i] == nil || chain[i+1] == nil {
			return fmt.Errorf("verified certificate path contains a nil certificate")
		}
		if err := checkCertificate(ctx, client, chain[i], chain[i+1], now); err != nil {
			if errors.Is(err, ErrRevoked) {
				return err
			}
			unavailable = err
		}
	}
	return unavailable
}

func checkCertificate(ctx context.Context, client *http.Client, cert, issuer *x509.Certificate, now time.Time) error {
	var lastErr error
	good := false
	for _, endpoint := range cert.OCSPServer[:min(len(cert.OCSPServer), 3)] {
		requestDER, err := ocsp.CreateRequest(cert, issuer, &ocsp.RequestOptions{Hash: crypto.SHA1})
		if err != nil {
			return err
		}
		responseDER, err := fetch(ctx, client, http.MethodPost, endpoint, "application/ocsp-request", requestDER)
		if err != nil {
			lastErr = err
			continue
		}
		response, err := ocsp.ParseResponseForCert(responseDER, cert, issuer)
		if err != nil || response.ThisUpdate.After(now.Add(5*time.Minute)) || response.NextUpdate.IsZero() || !now.Before(response.NextUpdate) {
			lastErr = fmt.Errorf("OCSP response is invalid or stale")
			continue
		}
		switch response.Status {
		case ocsp.Good:
			good = true
		case ocsp.Revoked:
			return ErrRevoked
		default:
			lastErr = fmt.Errorf("OCSP responder returned unknown status")
		}
	}
	for _, endpoint := range cert.CRLDistributionPoints[:min(len(cert.CRLDistributionPoints), 3)] {
		crlDER, err := fetch(ctx, client, http.MethodGet, endpoint, "", nil)
		if err != nil {
			lastErr = err
			continue
		}
		if strings.HasPrefix(string(crlDER), "-----BEGIN") {
			block, _ := pem.Decode(crlDER)
			if block == nil || block.Type != "X509 CRL" {
				lastErr = fmt.Errorf("invalid PEM CRL")
				continue
			}
			crlDER = block.Bytes
		}
		crl, err := x509.ParseRevocationList(crlDER)
		if err != nil || crl.CheckSignatureFrom(issuer) != nil || crl.ThisUpdate.After(now.Add(5*time.Minute)) || crl.NextUpdate.IsZero() || !now.Before(crl.NextUpdate) {
			lastErr = fmt.Errorf("CRL is invalid or stale")
			continue
		}
		isDeltaCRL := false
		for _, extension := range crl.Extensions {
			if extension.Id.Equal(oids.OidDeltaCRLIndicator) {
				isDeltaCRL = true
				break
			}
		}
		if isDeltaCRL {
			// A delta lists only changes, so it cannot determine status by itself.
			// TODO: Add base+delta CRL processing before using delta entries.
			// In particular, reason code 8 (removeFromCRL) clears a prior hold;
			// it must not be treated as a revocation when applying a delta.
			lastErr = fmt.Errorf("delta CRL status is unsupported without its base CRL")
			continue
		}
		for _, entry := range crl.RevokedCertificateEntries {
			if cert.SerialNumber.Cmp(entry.SerialNumber) == 0 {
				return ErrRevoked
			}
		}
		good = true
	}
	if good {
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("certificate revocation status unavailable: %w", lastErr)
	}
	return fmt.Errorf("certificate has no OCSP or CRL location")
}

func dialPublicHost(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, candidate := range addresses {
		ip, ok := netip.AddrFromSlice(candidate.IP)
		if !ok || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return nil, fmt.Errorf("revocation host %q has no permitted public address", host)
}

func allowedURL(raw string) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, fmt.Errorf("revocation URL is too long")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invalid revocation URL")
	}
	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return nil, fmt.Errorf("revocation URL uses an unsupported port")
	}
	return parsed, nil
}

func fetch(ctx context.Context, client *http.Client, method, rawURL, contentType string, body []byte) ([]byte, error) {
	parsed, err := allowedURL(rawURL)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("revocation responder returned HTTP %d", response.StatusCode)
	}
	result, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(result) > maxResponseBytes {
		return nil, fmt.Errorf("revocation response is too large")
	}
	return result, nil
}
