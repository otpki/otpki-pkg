package cmp

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // SHA-1 remains behind the caller's explicit legacy policy.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/asn1"
	"errors"
	"fmt"
	"hash"

	"github.com/otpki/otpki-pkg/oids"
)

// MaxPBMIterations preserves the existing server bound on password derivation.
const MaxPBMIterations = 100_000

// ComputePBM computes legacy PasswordBasedMac. This does not implement PBMAC1.
func ComputePBM(secret, data []byte, parameters PBMParameter, allowSHA1 bool) ([]byte, error) {
	if parameters.IterationCount <= 0 || parameters.IterationCount > MaxPBMIterations {
		return nil, fmt.Errorf("invalid PBM iterationCount %d", parameters.IterationCount)
	}
	owf, err := OWFFromOID(parameters.OWF.Algorithm, allowSHA1)
	if err != nil {
		return nil, err
	}
	mac, err := MACFromOID(parameters.MAC.Algorithm, allowSHA1)
	if err != nil {
		return nil, err
	}
	key := append(append([]byte(nil), secret...), parameters.Salt...)
	for i := 0; i < parameters.IterationCount; i++ {
		h := owf()
		_, _ = h.Write(key)
		key = h.Sum(nil)
	}
	h := hmac.New(mac, key)
	_, _ = h.Write(data)
	return h.Sum(nil), nil
}

func OWFFromOID(oid asn1.ObjectIdentifier, allowSHA1 bool) (func() hash.Hash, error) {
	switch {
	case oid.Equal(oids.OidASN1SHA256):
		return sha256.New, nil
	case oid.Equal(oids.OidASN1SHA384):
		return sha512.New384, nil
	case oid.Equal(oids.OidASN1SHA512):
		return sha512.New, nil
	case oid.Equal(oids.OidASN1SHA1):
		if !allowSHA1 {
			return nil, errors.New("PBM: SHA-1 OWF not allowed")
		}
		return sha1.New, nil
	default:
		return nil, fmt.Errorf("unsupported PBM OWF OID %v", oid)
	}
}

func MACFromOID(oid asn1.ObjectIdentifier, allowSHA1 bool) (func() hash.Hash, error) {
	switch {
	case oid.Equal(oids.OidHMACWithSHA256):
		return sha256.New, nil
	case oid.Equal(oids.OidHMACWithSHA384):
		return sha512.New384, nil
	case oid.Equal(oids.OidHMACWithSHA512):
		return sha512.New, nil
	case oid.Equal(oids.OidHMACWithSHA1_PKCS),
		oid.Equal(oids.OidHMACWithSHA1_IKE):
		if !allowSHA1 {
			return nil, errors.New("PBM: HMAC-SHA1 not allowed")
		}
		return sha1.New, nil
	default:
		return nil, fmt.Errorf("unsupported PBM MAC OID %v", oid)
	}
}
