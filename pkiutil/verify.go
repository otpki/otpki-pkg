package pkiutil

import (
	"crypto"

	"github.com/otpki/iogo/crypto/x509"
)

// VerifySignature checks a signature without making a certificate trust or
// authorization decision. Protocol callers retain those responsibilities.
func VerifySignature(pub crypto.PublicKey, algorithm x509.SignatureAlgorithm, message, signature []byte) error {
	return (&x509.Certificate{PublicKey: pub}).CheckSignature(algorithm, message, signature)
}
