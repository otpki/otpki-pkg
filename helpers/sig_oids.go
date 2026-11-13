package helpers

import (
	"crypto"
	"crypto/x509/pkix"
	"encoding/asn1"

	"github.com/otpki/iogo/crypto/x509"
	"github.com/otpki/otpki-pkg/pkiutil"
)

func AlgIDFromSigAlg(sa x509.SignatureAlgorithm) (asn1.ObjectIdentifier, *asn1.RawValue, error) {
	return pkiutil.AlgIDFromSigAlg(sa)
}

func SigAlgFromAlgID(oid asn1.ObjectIdentifier, params *asn1.RawValue) (x509.SignatureAlgorithm, error) {
	return pkiutil.SigAlgFromAlgID(oid, params)
}

func GetAlgoAndHash(pub crypto.PublicKey, oid *asn1.ObjectIdentifier) (pkix.AlgorithmIdentifier, crypto.Hash, error) {
	return pkiutil.GetAlgoAndHash(pub, oid)
}
