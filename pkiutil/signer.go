package pkiutil

import (
	"crypto"
	"encoding/asn1"
	"fmt"

	"github.com/otpki/iogo/crypto/x509"
	"github.com/otpki/otpki-pkg/oids"
)

func SignerAlgorithmFromPublicKey(pub crypto.PublicKey) (AlgorithmIdentifier, crypto.Hash, error) {
	alg, hash, err := GetAlgoAndHash(pub, nil)
	if err == nil {
		return AlgorithmIdentifier{
			Algorithm:  alg.Algorithm,
			Parameters: alg.Parameters,
		}, hash, nil
	}

	spki, spkiErr := SubjectPublicKeyInfoFromPublicKey(pub)
	if spkiErr != nil {
		return AlgorithmIdentifier{}, 0, err
	}

	sigAlg, sigErr := SigAlgFromAlgID(spki.Algorithm.Algorithm, &spki.Algorithm.Parameters)
	if sigErr != nil || !isHashlessSignatureAlgorithm(sigAlg) {
		return AlgorithmIdentifier{}, 0, err
	}

	return spki.Algorithm, crypto.Hash(0), nil
}

func SubjectPublicKeyInfoFromPublicKey(pub crypto.PublicKey) (SubjectPublicKeyInfo, error) {
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err == nil {
		var spki SubjectPublicKeyInfo
		if _, unmarshalErr := asn1.Unmarshal(pubDER, &spki); unmarshalErr != nil {
			return SubjectPublicKeyInfo{}, unmarshalErr
		}
		return spki, nil
	}

	type pqScheme interface {
		Name() string
	}
	type pqPublicKey interface {
		MarshalBinary() ([]byte, error)
		Scheme() pqScheme
	}

	pk, ok := pub.(pqPublicKey)
	if !ok {
		return SubjectPublicKeyInfo{}, err
	}

	keyBytes, marshalErr := pk.MarshalBinary()
	if marshalErr != nil {
		return SubjectPublicKeyInfo{}, marshalErr
	}

	oid, oidErr := pqPublicKeyOIDByName(pk.Scheme().Name())
	if oidErr != nil {
		return SubjectPublicKeyInfo{}, oidErr
	}

	return SubjectPublicKeyInfo{
		Algorithm: AlgorithmIdentifier{
			Algorithm: oid,
		},
		SubjectPublicKey: asn1.BitString{
			Bytes:     keyBytes,
			BitLength: len(keyBytes) * 8,
		},
	}, nil
}

func pqPublicKeyOIDByName(name string) (asn1.ObjectIdentifier, error) {
	switch name {
	case "ML-DSA-44":
		return oids.OidMLDSA44, nil
	case "ML-DSA-65":
		return oids.OidMLDSA65, nil
	case "ML-DSA-87":
		return oids.OidMLDSA87, nil
	case "SLH-DSA-SHA2-128s":
		return oids.OidSLHDSASHA2_128s, nil
	case "SLH-DSA-SHAKE-128s":
		return oids.OidSLHDSASHAKE_128s, nil
	case "SLH-DSA-SHA2-128f":
		return oids.OidSLHDSASHA2_128f, nil
	case "SLH-DSA-SHAKE-128f":
		return oids.OidSLHDSASHAKE_128f, nil
	case "SLH-DSA-SHA2-192s":
		return oids.OidSLHDSASHA2_192s, nil
	case "SLH-DSA-SHAKE-192s":
		return oids.OidSLHDSASHAKE_192s, nil
	case "SLH-DSA-SHA2-192f":
		return oids.OidSLHDSASHA2_192f, nil
	case "SLH-DSA-SHAKE-192f":
		return oids.OidSLHDSASHAKE_192f, nil
	case "SLH-DSA-SHA2-256s":
		return oids.OidSLHDSASHA2_256s, nil
	case "SLH-DSA-SHAKE-256s":
		return oids.OidSLHDSASHAKE_256s, nil
	case "SLH-DSA-SHA2-256f":
		return oids.OidSLHDSASHA2_256f, nil
	case "SLH-DSA-SHAKE-256f":
		return oids.OidSLHDSASHAKE_256f, nil
	default:
		return nil, fmt.Errorf("unsupported PQ public key scheme %q", name)
	}
}
