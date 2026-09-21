package helpers

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"strings"

	"github.com/otpki/iogo/crypto/x509"

	"github.com/samber/oops"
	"github.com/smallstep/pkcs7"
)

type EmptyHash struct{}

const ServiceName = "helpers"

func (e EmptyHash) HashFunc() crypto.Hash {
	return crypto.Hash(0)
}

func ToSigner(key any) (crypto.Signer, error) {
	pk, ok := key.(crypto.Signer)
	if !ok {
		return nil, oops.In(ServiceName).New("failed to get signer from key")
	}
	return pk, nil
}

//func ToPublic(parsedPk any) (crypto.PublicKey, error) {
//	pk, ok := parsedPk.(crypto.PrivateKey)
//	if !ok {
//		return nil, oops.Errorf("failed to get private key from pem")
//	}
//	return keyutil.PublicKey(pk)
//}

func ToDecrypter(parsedPk any) (crypto.Decrypter, error) {
	pk, ok := parsedPk.(crypto.Decrypter)
	if !ok {
		return nil, oops.In(ServiceName).New("failed to get decrypter from parsed pem")
	}
	return pk, nil
}

//func GenerateSerialNumber(bits uint) (*big.Int, error) {
//	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), bits)
//	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
//	return serialNumber, err
//}

func GenerateCertThumbprintSHA1(cert *x509.Certificate) string {
	digest := sha1.Sum(cert.Raw)
	return hex.EncodeToString(digest[:])
}

func GenerateFingerprint(publicKey crypto.PublicKey) (string, error) {
	hasher := sha256.New()

	pubBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", oops.In(ServiceName).Wrapf(err, "marshalling public key to bytes")
	}

	_, err = hasher.Write(pubBytes)
	if err != nil {
		return "", oops.In(ServiceName).Wrapf(err, "writing public key bytes to hash")
	}
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil)), nil
}

func GenerateRandomURLSafeString(length int64) (string, error) {
	if length <= 0 {
		return "", oops.In(ServiceName).New("length must be positive")
	}
	randomBytes := make([]byte, length)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", oops.In(ServiceName).Wrapf(err, "generating random bytes")
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes)[:length], nil
}

func GetRawPublicKeyBitStr(publicKey crypto.PublicKey) (asn1.BitString, error) {
	spkiBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return asn1.BitString{}, oops.In(ServiceName).Wrapf(err, "marshalling public key to SPKI DER")
	}

	var spki struct {
		Algorithm        pkix.AlgorithmIdentifier
		SubjectPublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(spkiBytes, &spki); err != nil {
		return asn1.BitString{}, oops.In(ServiceName).Wrapf(err, "unmarshalling SPKI")
	}
	return spki.SubjectPublicKey, nil
}

//func PrivateKeyToPEM(privateKey crypto.PrivateKey) ([]byte, error) {
//	var pemBlock *pem.Block
//	switch k := privateKey.(type) {
//	case *rsa.PrivateKey:
//		pemBlock = &pem.Block{
//			Type:  "RSA PRIVATE KEY",
//			Bytes: x509.MarshalPKCS1PrivateKey(k),
//		}
//	case *ecdsa.PrivateKey:
//		fmt.Printf("Ec not yet implemented, its on the list")
//		return nil, oops.Errorf("NOT IMPLEMENTED")
//	default:
//		return nil, oops.Errorf("UNSUPPORTED KEY TYPE")
//	}
//
//	return pem.EncodeToMemory(pemBlock), nil
//}
//
//func PEMtoDER(pemBytes []byte) ([]byte, error) {
//	// Decode pem block
//	block, _ := pem.Decode(pemBytes)
//	if block == nil {
//		return nil, oops.Errorf("FAILED TO PARSE PEM BLOCK")
//	}
//
//	// Return der bytes
//	return block.Bytes, nil
//}

func LoadCertificateFromPEMFile(certFile string) (*x509.Certificate, error) {
	// Read pem
	pemData, err := os.ReadFile(certFile)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "reading certificate file %s", certFile)
	}

	// Decode from file back to pem block
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, oops.In(ServiceName).New("failed to decode PEM data")
	}

	// Parse block bytes to x509
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "parsing certificate")
	}

	return cert, nil
}

func LoadCertificateChainFromPem(pemData []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate

	if len(pemData) > 0 {
		for {
			block, rest := pem.Decode(pemData)
			if block == nil {
				break
			}
			if block.Type == "CERTIFICATE" {
				cert, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					return nil, oops.In(ServiceName).Wrapf(err, "parsing certificate from PEM")
				}
				certs = append(certs, cert)
			}
			pemData = rest
		}
		if len(certs) == 0 { // Should err if there is data, but nothing parsed
			return nil, oops.In(ServiceName).New("no certificates found in PEM data")
		}
	}

	return certs, nil
}

func LoadCertificateChainFromP7b(p7bData []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate

	if len(p7bData) > 0 {
		if block, _ := pem.Decode(p7bData); block != nil && block.Type == "PKCS7" {
			p7bData = block.Bytes
		}
		p7, err := pkcs7.Parse(p7bData)
		if err != nil {
			return nil, oops.In(ServiceName).Wrapf(err, "parsing P7B chain")
		}
		// Convert stdlib x509 to
		convertedCerts := make([]*x509.Certificate, len(p7.Certificates))
		for _, c := range p7.Certificates {
			cert, err := x509.CertificateFromStdX509(c)
			if err != nil {
				return nil, oops.In(ServiceName).New("failed to parse x509 cert to iogo x509 cert")
			}
			convertedCerts = append(convertedCerts, cert)
		}
		certs = append(certs, convertedCerts...)
		if len(certs) == 0 { // Should err if there is data, but nothing parsed
			return nil, oops.In(ServiceName).New("no certificates found in P7B data")
		}
	}

	return certs, nil
}

func GeneratePassword(length int64, charset []string) (string, error) {
	// Create a single character pool from the slice of strings.
	var charPoolBuilder strings.Builder
	for _, s := range charset {
		charPoolBuilder.WriteString(s)
	}
	charPool := charPoolBuilder.String()

	// if the length is zero or the character pool is empty, return an empty string without an error.
	if length == 0 || len(charPool) == 0 {
		return "", nil
	}

	password := make([]byte, length)
	charsetLength := big.NewInt(int64(len(charPool))) // since rand uses bigint

	for i := range length {
		randomIndex, err := rand.Int(rand.Reader, charsetLength)
		if err != nil {
			return "", oops.In(ServiceName).Wrapf(err, "generating random index")
		}
		password[i] = charPool[randomIndex.Int64()]
	}

	return string(password), nil
}

func ParseCertificateChainFromHeaderValue(headerValue string) ([]*x509.Certificate, error) {
	if headerValue == "" {
		return nil, oops.In(ServiceName).New("missing client certificate header")
	}

	if strings.Contains(headerValue, "%") { // AWS case: https://aws.amazon.com/blogs/networking-and-content-delivery/introducing-mtls-for-application-load-balancer/
		// url encoded pem string: -----BEGIN%20CERTIFICATE-----%0AMII...
		decoded, err := url.PathUnescape(headerValue)
		if err != nil {
			return nil, oops.In(ServiceName).Wrapf(err, "decoding certificate header")
		}
		headerValue = decoded
	} else if !strings.HasPrefix("-----BEGIN", headerValue) { // Traefik case: https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/passtlsclientcert/#data-format
		// Delimiters removed: no PEM header/trailer characters and line breaks
		// X-Client-Certificate:[MIICLjCCARagA....0pkNplWJA==,MIIC+DCCAeCgAwIBA....5YDOXJqv2p2aXA=]
		// Below reconstructs a pem for each comma separated base64 der segment
		chain := ""
		for segment := range strings.SplitSeq(headerValue, ",") {
			der, err := base64.StdEncoding.DecodeString(segment)
			if err != nil {
				return nil, oops.Errorf("failed to decode certificate header: %w", err)
			}
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				return nil, oops.Errorf("failed to parse der bytes:%w", err)
			}
			pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
			chain = chain + string(pemCert)
		}
		headerValue = chain
	}
	// Else: pem chain or single cert
	return LoadCertificateChainFromPemNormalized([]byte(headerValue))
}

func LoadCertificateChainFromPemNormalized(pemData []byte) ([]*x509.Certificate, error) {
	certs, err := LoadCertificateChainFromPem(pemData)
	if err != nil {
		return nil, err
	}
	return NormalizeCertificateOrder(certs), nil
}

func NormalizeCertificateOrder(certs []*x509.Certificate) []*x509.Certificate {
	if len(certs) <= 1 {
		return certs
	}

	ordered := make([]*x509.Certificate, 0, len(certs))
	used := make([]bool, len(certs))

	currentIdx := max(findLeafIndex(certs), 0)

	for currentIdx >= 0 && !used[currentIdx] {
		ordered = append(ordered, certs[currentIdx])
		used[currentIdx] = true
		currentIdx = findIssuerIndex(certs, currentIdx, used)
	}

	for i := range certs {
		if !used[i] {
			ordered = append(ordered, certs[i])
		}
	}

	return ordered
}

func findLeafIndex(certs []*x509.Certificate) int {
	for i := range certs {
		isIssuer := false
		for j := range certs {
			if i == j {
				continue
			}
			if isIssuerOf(certs[i], certs[j]) {
				isIssuer = true
				break
			}
		}
		if !isIssuer {
			return i
		}
	}
	return -1
}

func findIssuerIndex(certs []*x509.Certificate, currentIdx int, used []bool) int {
	current := certs[currentIdx]
	for i := range certs {
		if used[i] || i == currentIdx {
			continue
		}
		if isIssuerOf(certs[i], current) {
			return i
		}
	}
	return -1
}

func isIssuerOf(issuer, subject *x509.Certificate) bool {
	if issuer == nil || subject == nil {
		return false
	}

	if subject.Issuer.String() != issuer.Subject.String() {
		return false
	}

	if len(subject.AuthorityKeyId) > 0 && len(issuer.SubjectKeyId) > 0 {
		if !bytes.Equal(subject.AuthorityKeyId, issuer.SubjectKeyId) {
			return false
		}
	}

	if err := subject.CheckSignatureFrom(issuer); err != nil {
		return false
	}

	return true
}
