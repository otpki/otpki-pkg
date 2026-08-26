package helpers

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"net"
	"net/url"
	"unicode"

	"github.com/otpki/otpki-pkg/oids"
	"github.com/samber/oops"
	"golang.org/x/crypto/cryptobyte"
	cryptobyteasn1 "golang.org/x/crypto/cryptobyte/asn1"
)

// TODO: ISSPKI-106
//type ChangeSubjectName struct {
//	Subject    pkix.RDNSequence `asn1:"optional"`
//	SubjectAlt *pkix.Extension  `asn1:"optional"`
//}

// Returns empty pkix.Extension if not found
func FindExtension(exts []pkix.Extension, oid asn1.ObjectIdentifier) pkix.Extension {
	for _, v := range exts {
		if v.Id.Equal(oid) {
			return v
		}
	}
	return pkix.Extension{}
}

func AddOrUpdateExtension(exts []pkix.Extension, oid asn1.ObjectIdentifier, value []byte) []pkix.Extension {
	for i := range exts {
		if exts[i].Id.Equal(oid) {
			exts[i].Value = value
			return exts
		}
	}
	exts = append(exts, pkix.Extension{Id: oid, Value: value})
	return exts
}

func RemoveExtension(exts []pkix.Extension, oid asn1.ObjectIdentifier) []pkix.Extension {
	pruned := []pkix.Extension{}
	for _, v := range exts {
		if !oid.Equal(v.Id) {
			pruned = append(pruned, v)
		}
	}
	return pruned
}

func IsIA5String(s string) bool {
	for _, r := range s {
		// Per RFC5280 "IA5String is limited to the set of ASCII characters"
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func GetKeyUsageDER(usage x509.KeyUsage) ([]byte, error) {
	var a [2]byte
	a[0] = ReverseBitsInAByte(byte(usage))
	a[1] = ReverseBitsInAByte(byte(usage >> 8))

	l := 1
	if a[1] != 0 {
		l = 2
	}

	bitString := a[:l]
	return asn1.Marshal(asn1.BitString{Bytes: bitString, BitLength: ASN1BitLength(bitString)})
}

func GetExtKeyUsageDER(eku []x509.ExtKeyUsage, extraEku []asn1.ObjectIdentifier) ([]byte, error) {
	oids := make([]asn1.ObjectIdentifier, len(eku)+len(extraEku))
	for i, u := range eku {
		if oid, ok := oidFromExtKeyUsage(u); ok {
			oids[i] = oid
		} else {
			return nil, oops.In(ServiceName).New("x509: unknown extended key usage")
		}
	}

	copy(oids[len(eku):], extraEku)
	return asn1.Marshal(oids)
}

// extKeyUsageOIDs contains the mapping between an ExtKeyUsage and its OID.
var extKeyUsageOIDs = []struct {
	extKeyUsage x509.ExtKeyUsage
	oid         asn1.ObjectIdentifier
}{
	{x509.ExtKeyUsageAny, oids.OidExtKeyUsageAny},
	{x509.ExtKeyUsageServerAuth, oids.OidExtKeyUsageServerAuth},
	{x509.ExtKeyUsageClientAuth, oids.OidExtKeyUsageClientAuth},
	{x509.ExtKeyUsageCodeSigning, oids.OidExtKeyUsageCodeSigning},
	{x509.ExtKeyUsageEmailProtection, oids.OidExtKeyUsageEmailProtection},
	{x509.ExtKeyUsageIPSECEndSystem, oids.OidExtKeyUsageIPSECEndSystem},
	{x509.ExtKeyUsageIPSECTunnel, oids.OidExtKeyUsageIPSECTunnel},
	{x509.ExtKeyUsageIPSECUser, oids.OidExtKeyUsageIPSECUser},
	{x509.ExtKeyUsageTimeStamping, oids.OidExtKeyUsageTimeStamping},
	{x509.ExtKeyUsageOCSPSigning, oids.OidExtKeyUsageOCSPSigning},
	{x509.ExtKeyUsageMicrosoftServerGatedCrypto, oids.OidExtKeyUsageMicrosoftServerGatedCrypto},
	{x509.ExtKeyUsageNetscapeServerGatedCrypto, oids.OidExtKeyUsageNetscapeServerGatedCrypto},
	{x509.ExtKeyUsageMicrosoftCommercialCodeSigning, oids.OidExtKeyUsageMicrosoftCommercialCodeSigning},
	{x509.ExtKeyUsageMicrosoftKernelCodeSigning, oids.OidExtKeyUsageMicrosoftKernelCodeSigning},
}

// func extKeyUsageFromOID(oid asn1.ObjectIdentifier) (eku x509.ExtKeyUsage, ok bool) {
// 	for _, pair := range extKeyUsageOIDs {
// 		if oid.Equal(pair.oid) {
// 			return pair.extKeyUsage, true
// 		}
// 	}
// 	return
// }

func oidFromExtKeyUsage(eku x509.ExtKeyUsage) (oid asn1.ObjectIdentifier, ok bool) {
	for _, pair := range extKeyUsageOIDs {
		if eku == pair.extKeyUsage {
			return pair.oid, true
		}
	}
	return
}

func NameConstraintsToPKIX(pDNS []string, pIP []string, pEmail []string, pURI []string, eDNS []string, eIP []string, eEmail []string, eURI []string, extCritical bool) (*pkix.Extension, error) {
	ext := &pkix.Extension{}
	ext.Id = oids.OidNameConstraints
	ext.Critical = extCritical

	ipAndMask := func(ipNet string) ([]byte, error) {
		_, parsedIPNet, err := net.ParseCIDR(ipNet)
		if err != nil || parsedIPNet == nil {
			return nil, err
		}
		maskedIP := parsedIPNet.IP.Mask(parsedIPNet.Mask)
		ipAndMask := make([]byte, 0, len(maskedIP)+len(parsedIPNet.Mask))
		ipAndMask = append(ipAndMask, maskedIP...)
		ipAndMask = append(ipAndMask, parsedIPNet.Mask...)
		return ipAndMask, nil
	}

	serialiseConstraints := func(dns []string, ips []string, emails []string, uriDomains []string) (der []byte, err error) {
		var b cryptobyte.Builder

		for _, name := range dns {
			if !IsIA5String(name) {
				return nil, oops.In(ServiceName).Errorf("invalid DNS name: %s", name)
			}

			b.AddASN1(cryptobyteasn1.SEQUENCE, func(b *cryptobyte.Builder) {
				b.AddASN1(cryptobyteasn1.Tag(2).ContextSpecific(), func(b *cryptobyte.Builder) {
					b.AddBytes([]byte(name))
				})
			})
		}

		for _, ipNet := range ips {
			b.AddASN1(cryptobyteasn1.SEQUENCE, func(b *cryptobyte.Builder) {
				b.AddASN1(cryptobyteasn1.Tag(7).ContextSpecific(), func(b *cryptobyte.Builder) {
					var ipBytes []byte
					ipBytes, err = ipAndMask(ipNet)
					if err != nil {
						return
					}
					if len(ipBytes) == 0 {
						err = oops.In(ServiceName).Errorf("invalid IP network: %s", ipNet)
						return
					}
					b.AddBytes(ipBytes)
				})
			})
		}

		for _, email := range emails {
			if !IsIA5String(email) {
				return nil, oops.In(ServiceName).Errorf("invalid email: %s", email)
			}

			b.AddASN1(cryptobyteasn1.SEQUENCE, func(b *cryptobyte.Builder) {
				b.AddASN1(cryptobyteasn1.Tag(1).ContextSpecific(), func(b *cryptobyte.Builder) {
					b.AddBytes([]byte(email))
				})
			})
		}

		for _, uriDomain := range uriDomains {
			if !IsIA5String(uriDomain) {
				return nil, oops.In(ServiceName).Errorf("invalid URI domain: %s", uriDomain)
			}

			b.AddASN1(cryptobyteasn1.SEQUENCE, func(b *cryptobyte.Builder) {
				b.AddASN1(cryptobyteasn1.Tag(6).ContextSpecific(), func(b *cryptobyte.Builder) {
					b.AddBytes([]byte(uriDomain))
				})
			})
		}

		return b.Bytes()
	}

	permitted, err := serialiseConstraints(pDNS, pIP, pEmail, pURI)
	if err != nil {
		return nil, err
	}

	excluded, err := serialiseConstraints(eDNS, eIP, eEmail, eURI)
	if err != nil {
		return nil, err
	}

	var b cryptobyte.Builder
	b.AddASN1(cryptobyteasn1.SEQUENCE, func(b *cryptobyte.Builder) {
		if len(permitted) > 0 {
			b.AddASN1(cryptobyteasn1.Tag(0).ContextSpecific().Constructed(), func(b *cryptobyte.Builder) {
				b.AddBytes(permitted)
			})
		}

		if len(excluded) > 0 {
			b.AddASN1(cryptobyteasn1.Tag(1).ContextSpecific().Constructed(), func(b *cryptobyte.Builder) {
				b.AddBytes(excluded)
			})
		}
	})

	if ext.Value == nil {
		return nil, nil
	}
	return ext, nil
}

func ValidateChainOfTrust(certs []*x509.Certificate) (*x509.Certificate, []*x509.Certificate, error) {
	if len(certs) == 0 {
		return nil, nil, oops.In(ServiceName).New("chain of trust validation failed: certificates cannot be empty")
	}

	// Build roots + intermediates + unique cert count
	roots := x509.NewCertPool()
	intermediates := x509.NewCertPool()
	seen := map[string]bool{}
	certCount := 0
	for _, c := range certs {
		id := string(c.Raw)
		if !seen[id] {
			seen[id] = true
			certCount++
		} else {
			continue
		}

		if c.CheckSignatureFrom(c) == nil {
			roots.AddCert(c)
		} else {
			intermediates.AddCert(c)
		}
	}

	// Verify against root+intermediates
	for _, c := range certs {
		chains, err := c.Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		})
		if err == nil {
			if len(chains[0]) == certCount { // Use chain len to find "leaf" / invalidate mutli chains
				return chains[0][0], chains[0][1:], nil
			}
		}
	}

	return nil, nil, oops.In(ServiceName).New("chain of trust validation failed: unable to build a valid chain")
}

const (
	nameTagEmail     = 1
	nameTagDNS       = 2
	nameTagURI       = 6
	nameTagIP        = 7
	nameTagOtherName = 0
)

// sanOtherName represents the ASN.1 structure of an otherName SAN component
type sanOtherName struct {
	ID    asn1.ObjectIdentifier
	Value asn1.RawValue `asn1:"explicit,tag:0"`
}

type KRB5PrincipalName struct {
	Realm         string `asn1:"explicit,tag:0"`
	PrincipalName string `asn1:"explicit,tag:1"`
}

type HwModuleName struct {
	HwType      asn1.ObjectIdentifier
	HwSerialNum []byte
}

type DistributionPoint struct {
	DistributionPoint DistributionPointName `asn1:"optional,tag:0"`
	Reason            asn1.BitString        `asn1:"optional,tag:1"`
	CRLIssuer         asn1.RawValue         `asn1:"optional,tag:2"`
}

type DistributionPointName struct {
	FullName     []asn1.RawValue  `asn1:"optional,tag:0"`
	RelativeName pkix.RDNSequence `asn1:"optional,tag:1"`
}

// OtherName represents an otherName entry in the Subject Alternative Name extension
type OtherName struct {
	TypeID     asn1.ObjectIdentifier
	Value      string // Used when RawValue is nil
	RawValue   []byte // Pre-encoded ASN.1 value, used for complex structures
	Structured any    // Can be used to hold a structured value like KRB5PrincipalName
}

// BuildSubjectAltNameExtension creates a DER encoded SubjectAlternativeName extension
// that includes the provided DNS names, email addresses, IP addresses, URIs, and otherName entries
func BuildSubjectAltNameExtension(dnsNames []string, emails []string, ips []net.IP, uris []*url.URL, others []*OtherName) ([]byte, error) {
	var rawValues []asn1.RawValue
	for _, name := range dnsNames {
		if !IsIA5String(name) {
			return nil, oops.In(ServiceName).New("invalid dns name")
		}
		rawValues = append(rawValues, asn1.RawValue{Tag: nameTagDNS, Class: 2, Bytes: []byte(name)})
	}
	for _, email := range emails {
		if !IsIA5String(email) {
			return nil, oops.In(ServiceName).New("invalid email")
		}
		rawValues = append(rawValues, asn1.RawValue{Tag: nameTagEmail, Class: 2, Bytes: []byte(email)})
	}
	for _, rawIP := range ips {
		// If possible, we always want to encode IPv4 addresses in 4 bytes.
		ip := rawIP.To4()
		if ip == nil {
			ip = rawIP
		}
		rawValues = append(rawValues, asn1.RawValue{Tag: nameTagIP, Class: 2, Bytes: ip})
	}
	for _, uri := range uris {
		uriStr := uri.String()
		if !IsIA5String(uriStr) {
			return nil, oops.In(ServiceName).New("invalid uri")
		}
		rawValues = append(rawValues, asn1.RawValue{Tag: nameTagURI, Class: 2, Bytes: []byte(uriStr)})
	}

	// Add otherName entries
	for _, other := range others {
		// Create the OtherName ASN.1 structure
		otherNameValue := asn1.RawValue{
			Class:      2, // Context-specific class (2)
			Tag:        nameTagOtherName,
			IsCompound: true,
		}

		var err error

		// Handle different ways of providing otherName values
		if len(other.RawValue) > 0 {
			// Use pre-encoded ASN.1 value directly
			otherNameValue.Bytes = other.RawValue
		} else if other.Structured != nil {
			// Marshal a structured value
			otherNameValue.Bytes, err = asn1.Marshal(other.Structured)
			if err != nil {
				return nil, oops.In(ServiceName).Wrapf(err, "marshalling otherName structured value")
			}
		} else {
			// For other types, use the value directly
			otherNameValue.Bytes, err = asn1.Marshal(other.Value)
			if err != nil {
				return nil, oops.In(ServiceName).Wrapf(err, "marshalling otherName value")
			}
		}

		// Create the otherName structure with the type ID and value
		otherName := sanOtherName{
			ID:    other.TypeID,
			Value: otherNameValue,
		}

		// Marshal the entire otherName structure
		raw, err := asn1.Marshal(otherName)
		if err != nil {
			return nil, oops.In(ServiceName).Wrapf(err, "marshalling otherName")
		}
		var sequenceContent asn1.RawValue
		_, err = asn1.Unmarshal(raw, &sequenceContent)
		if err != nil {
			return nil, oops.In(ServiceName).Wrapf(err, "unmarshalling otherName sequence")
		}

		// Add to the list of raw values with context-specific tag 0
		rawValues = append(rawValues, asn1.RawValue{
			Class:      2, // Context-specific class (2)
			Tag:        nameTagOtherName,
			IsCompound: true,
			Bytes:      sequenceContent.Bytes,
		})
	}

	// If we have no values, return empty
	if len(rawValues) == 0 {
		return nil, nil
	}

	// Return the complete SAN extension value
	return asn1.Marshal(rawValues)
}

func MarshalCertificateChain(chain []*x509.Certificate) [][]byte {
	certs := make([][]byte, len(chain))
	for i, cert := range chain {
		certs[i] = cert.Raw
	}
	return certs
}

func UnmarshalCertificateChain(certs [][]byte) ([]*x509.Certificate, error) {
	chain := make([]*x509.Certificate, len(certs))
	for i, cert := range certs {
		c, err := x509.ParseCertificate(cert)
		if err != nil {
			return nil, err
		}
		chain[i] = c
	}
	return chain, nil
}

// UnmarshalCertificateChainAndCert combines the given certificate and certificate chain into a single slice. The cert chain
// is in order from leaf to root
func UnmarshalCertificateChainAndCert(certificate []byte, certificateChain [][]byte) ([]*x509.Certificate, error) {
	certs := append([][]byte{certificate}, certificateChain...)
	return UnmarshalCertificateChain(certs)
}
