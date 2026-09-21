package dnssec

import (
	"bytes"
	"context"
	stdX509 "crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/otpki/iogo/crypto/x509"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/rdata"
	"github.com/smallstep/pkcs7"
)

const (
	ianaRootTrustAnchorURL          = "https://data.iana.org/root-anchors/root-anchors.xml"
	ianaRootTrustAnchorSignatureURL = "https://data.iana.org/root-anchors/root-anchors.p7s"
	maxIANATrustAnchorFileSize      = 1 << 20
)

//go:embed icannbundle.pem
var icannRootCAsPEM []byte

type ianaTrustAnchorDocument struct {
	XMLName    xml.Name                   `xml:"TrustAnchor"`
	Zone       string                     `xml:"Zone"`
	KeyDigests []ianaTrustAnchorKeyDigest `xml:"KeyDigest"`
}

type ianaTrustAnchorKeyDigest struct {
	ValidFrom  string  `xml:"validFrom,attr"`
	ValidUntil string  `xml:"validUntil,attr"`
	KeyTag     uint16  `xml:"KeyTag"`
	Algorithm  uint8   `xml:"Algorithm"`
	DigestType uint8   `xml:"DigestType"`
	Digest     string  `xml:"Digest"`
	PublicKey  string  `xml:"PublicKey"`
	Flags      *uint16 `xml:"Flags"`
}

// RefreshIANARootTrustAnchors downloads IANA's DNSSEC root trust-anchor
// publication and detached CMS signature. It replaces the active defaults only
// after the signature, certificate chain, XML, validity windows, and optional
// DNSKEY-to-DS values all verify. A failed refresh leaves the existing anchors
// untouched.
func RefreshIANARootTrustAnchors(ctx context.Context, client *http.Client) ([]TrustAnchor, error) {
	if client == nil {
		client = http.DefaultClient
	}
	anchors, err := fetchIANARootTrustAnchors(
		ctx,
		client,
		ianaRootTrustAnchorURL,
		ianaRootTrustAnchorSignatureURL,
		icannRootCAsPEM,
		time.Now(),
	)
	if err != nil {
		return nil, err
	}
	replaceIANARootTrustAnchors(anchors)
	return append([]TrustAnchor(nil), anchors...), nil
}

func fetchIANARootTrustAnchors(
	ctx context.Context,
	client *http.Client,
	anchorURL string,
	signatureURL string,
	trustedRootsPEM []byte,
	now time.Time,
) ([]TrustAnchor, error) {
	anchorXML, err := downloadIANATrustAnchorFile(ctx, client, anchorURL)
	if err != nil {
		return nil, fmt.Errorf("download IANA root trust anchors: %w", err)
	}
	signature, err := downloadIANATrustAnchorFile(ctx, client, signatureURL)
	if err != nil {
		return nil, fmt.Errorf("download IANA root trust-anchor signature: %w", err)
	}
	if err := verifyIANATrustAnchorSignature(anchorXML, signature, trustedRootsPEM, now); err != nil {
		return nil, err
	}
	anchors, err := parseIANARootTrustAnchors(anchorXML, now)
	if err != nil {
		return nil, fmt.Errorf("parse IANA root trust anchors: %w", err)
	}
	return anchors, nil
}

func downloadIANATrustAnchorFile(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIANATrustAnchorFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("response is empty")
	}
	if len(data) > maxIANATrustAnchorFileSize {
		return nil, fmt.Errorf("response exceeds %d bytes", maxIANATrustAnchorFileSize)
	}
	return data, nil
}

func verifyIANATrustAnchorSignature(content, signature, trustedRootsPEM []byte, now time.Time) error {
	trustedRoots := stdX509.NewCertPool()
	if !trustedRoots.AppendCertsFromPEM(trustedRootsPEM) {
		return errors.New("parse embedded ICANN root certificates")
	}
	signedData, err := pkcs7.Parse(signature)
	if err != nil {
		return fmt.Errorf("parse IANA root trust-anchor signature: %w", err)
	}
	if len(signedData.Content) != 0 {
		return errors.New("IANA root trust-anchor signature is not detached")
	}
	signer := signedData.GetOnlySigner()
	if signer == nil {
		return errors.New("IANA root trust-anchor signature must contain exactly one signer")
	}
	x509Signer, err := x509.CertificateFromStdX509(signer)
	if err != nil {
		return fmt.Errorf("converting x509 certificate to iogo: %w", err)
	}
	if x509Signer.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return errors.New("IANA root trust-anchor signer is not permitted to create digital signatures")
	}
	if !hasExtendedKeyUsage(x509Signer, x509.ExtKeyUsageEmailProtection) {
		return errors.New("IANA root trust-anchor signer is not permitted to sign S/MIME content")
	}
	signedData.Content = content
	if err := signedData.VerifyWithChainAtTime(trustedRoots, now); err != nil {
		return fmt.Errorf("verify IANA root trust-anchor signature: %w", err)
	}
	return nil
}

func hasExtendedKeyUsage(certificate *x509.Certificate, usage x509.ExtKeyUsage) bool {
	for _, candidate := range certificate.ExtKeyUsage {
		if candidate == usage || candidate == x509.ExtKeyUsageAny {
			return true
		}
	}
	return false
}

func parseIANARootTrustAnchors(data []byte, now time.Time) ([]TrustAnchor, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var document ianaTrustAnchorDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if document.XMLName.Local != "TrustAnchor" {
		return nil, fmt.Errorf("unexpected root element %q", document.XMLName.Local)
	}
	if strings.TrimSpace(document.Zone) != "." {
		return nil, fmt.Errorf("unexpected trust-anchor zone %q", document.Zone)
	}
	if err := ensureXMLDocumentEnded(decoder); err != nil {
		return nil, err
	}

	anchors := make([]TrustAnchor, 0, len(document.KeyDigests))
	for i, keyDigest := range document.KeyDigests {
		anchor, active, err := keyDigest.trustAnchor(now)
		if err != nil {
			return nil, fmt.Errorf("KeyDigest %d: %w", i, err)
		}
		if active {
			anchors = append(anchors, anchor)
		}
	}
	if len(anchors) == 0 {
		return nil, errors.New("publication contains no currently valid root trust anchors")
	}
	return anchors, nil
}

func ensureXMLDocumentEnded(decoder *xml.Decoder) error {
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch token := token.(type) {
		case xml.CharData:
			if len(bytes.TrimSpace(token)) != 0 {
				return errors.New("unexpected content after TrustAnchor element")
			}
		case xml.Comment, xml.ProcInst:
		default:
			return errors.New("unexpected content after TrustAnchor element")
		}
	}
}

func (keyDigest ianaTrustAnchorKeyDigest) trustAnchor(now time.Time) (TrustAnchor, bool, error) {
	validFrom, err := time.Parse(time.RFC3339, keyDigest.ValidFrom)
	if err != nil {
		return TrustAnchor{}, false, fmt.Errorf("invalid validFrom: %w", err)
	}
	var validUntil time.Time
	if keyDigest.ValidUntil != "" {
		validUntil, err = time.Parse(time.RFC3339, keyDigest.ValidUntil)
		if err != nil {
			return TrustAnchor{}, false, fmt.Errorf("invalid validUntil: %w", err)
		}
		if !validUntil.After(validFrom) {
			return TrustAnchor{}, false, errors.New("validUntil must be after validFrom")
		}
	}
	active := !now.Before(validFrom) && (validUntil.IsZero() || now.Before(validUntil))
	if !active {
		return TrustAnchor{}, false, nil
	}

	digest := strings.ToUpper(strings.Join(strings.Fields(keyDigest.Digest), ""))
	digestBytes, err := hex.DecodeString(digest)
	if err != nil || len(digestBytes) == 0 {
		return TrustAnchor{}, false, errors.New("digest must be non-empty hexadecimal")
	}
	if (keyDigest.PublicKey == "") != (keyDigest.Flags == nil) {
		return TrustAnchor{}, false, errors.New("PublicKey and Flags must either both be present or both be absent")
	}
	if keyDigest.PublicKey != "" {
		if err := keyDigest.verifyPublicKeyDigest(digest); err != nil {
			return TrustAnchor{}, false, err
		}
	}

	return TrustAnchor{
		Owner:      ".",
		KeyTag:     keyDigest.KeyTag,
		Algorithm:  keyDigest.Algorithm,
		DigestType: keyDigest.DigestType,
		Digest:     digest,
	}, true, nil
}

func (keyDigest ianaTrustAnchorKeyDigest) verifyPublicKeyDigest(expectedDigest string) error {
	key := &dns.DNSKEY{
		Hdr: dns.Header{Name: ".", Class: dns.ClassINET},
		DNSKEY: rdata.DNSKEY{
			Flags:     *keyDigest.Flags,
			Protocol:  3,
			Algorithm: keyDigest.Algorithm,
			PublicKey: strings.Join(strings.Fields(keyDigest.PublicKey), ""),
		},
	}
	if key.KeyTag() != keyDigest.KeyTag {
		return errors.New("PublicKey does not match KeyTag")
	}
	ds := key.ToDS(keyDigest.DigestType)
	if ds == nil || !strings.EqualFold(ds.Digest, expectedDigest) {
		return errors.New("PublicKey does not match Digest")
	}
	return nil
}
