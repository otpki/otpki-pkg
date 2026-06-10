package helpers

import (
	"crypto"
	"crypto/circl"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"

	"github.com/otpki/otpki-pkg/oids"
	"github.com/samber/oops"
)

// ===== RSASSA-PSS params encode/decode =====

type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type rsassaPSSParams struct {
	HashAlgorithm    algorithmIdentifier `asn1:"tag:0,explicit,optional"`
	MaskGenAlgorithm algorithmIdentifier `asn1:"tag:1,explicit,optional"`
	SaltLength       int                 `asn1:"tag:2,explicit,optional"`
	TrailerField     int                 `asn1:"tag:3,explicit,optional"`
}

func pssParamsForHash(hashOID asn1.ObjectIdentifier, saltLen int) (*asn1.RawValue, error) {
	mgf1HashAI := algorithmIdentifier{Algorithm: hashOID}
	mgf1HashDER, err := asn1.Marshal(mgf1HashAI)
	if err != nil {
		return nil, err
	}
	params := rsassaPSSParams{
		HashAlgorithm: algorithmIdentifier{Algorithm: hashOID},
		MaskGenAlgorithm: algorithmIdentifier{
			Algorithm:  oids.OidMGF1,
			Parameters: asn1.RawValue{FullBytes: mgf1HashDER},
		},
		SaltLength:   saltLen,
		TrailerField: 1,
	}
	der, err := asn1.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &asn1.RawValue{FullBytes: der}, nil
}

func parsePSSParams(params *asn1.RawValue) (hashOID asn1.ObjectIdentifier, ok bool) {
	if params == nil || len(params.FullBytes) == 0 {
		return nil, false
	}
	var p rsassaPSSParams
	if _, err := asn1.Unmarshal(params.FullBytes, &p); err != nil {
		return nil, false
	}
	if len(p.HashAlgorithm.Algorithm) == 0 {
		return nil, false
	}
	return p.HashAlgorithm.Algorithm, true
}

// ===== Forward mapping: enum -> (OID, params) =====

// AlgIDFromSigAlg maps your x509.SignatureAlgorithm to a signature AlgorithmIdentifier parts.
// It intentionally excludes the enum values that were marked "// Unsupported." in your list.
func AlgIDFromSigAlg(sa x509.SignatureAlgorithm) (oid asn1.ObjectIdentifier, params *asn1.RawValue, err error) {
	switch sa {
	case x509.MD5WithRSA:
		return oids.OidMD5WithRSA, nil, nil
	case x509.SHA1WithRSA:
		return oids.OidSignatureSHA1WithRSA, nil, nil
	case x509.SHA256WithRSA:
		return oids.OidSignatureSHA256WithRSA, nil, nil
	case x509.SHA384WithRSA:
		return oids.OidSignatureSHA384WithRSA, nil, nil
	case x509.SHA512WithRSA:
		return oids.OidSignatureSHA512WithRSA, nil, nil

	case x509.ECDSAWithSHA1:
		return oids.OidSignatureECDSAWithSHA1, nil, nil
	case x509.ECDSAWithSHA256:
		return oids.OidSignatureECDSAWithSHA256, nil, nil
	case x509.ECDSAWithSHA384:
		return oids.OidSignatureECDSAWithSHA384, nil, nil
	case x509.ECDSAWithSHA512:
		return oids.OidSignatureECDSAWithSHA512, nil, nil

	case x509.SHA256WithRSAPSS:
		p, e := pssParamsForHash(oids.OidASN1SHA256, 32)
		return oids.OidRSASSAPSS, p, e
	case x509.SHA384WithRSAPSS:
		p, e := pssParamsForHash(oids.OidASN1SHA384, 48)
		return oids.OidRSASSAPSS, p, e
	case x509.SHA512WithRSAPSS:
		p, e := pssParamsForHash(oids.OidASN1SHA512, 64)
		return oids.OidRSASSAPSS, p, e

	case x509.PureEd25519:
		return oids.OidEd25519, nil, nil
	case x509.PureEd448:
		return oids.OidEd448, nil, nil

	// TODO Dilithium-named enums: ecosystem-specific OIDs
	case x509.PureEdDilithium2, x509.PureEdDilithium3, x509.PureDilithium2, x509.PureDilithium3, x509.PureDilithium5:
		return nil, nil, oops.In(ServiceName).Errorf("no stable OID mapping provided for %v (wire your own or prefer ML-DSA)", sa)

	case x509.PureMLDSA44:
		return oids.OidMLDSA44, nil, nil
	case x509.PureMLDSA65:
		return oids.OidMLDSA65, nil, nil
	case x509.PureMLDSA87:
		return oids.OidMLDSA87, nil, nil

	case x509.SLHDSAWithSHA128s:
		return oids.OidSLHDSASHA2_128s, nil, nil
	case x509.SLHDSAWithSHAKE128s:
		return oids.OidSLHDSASHAKE_128s, nil, nil
	case x509.SLHDSAWithSHA128f:
		return oids.OidSLHDSASHA2_128f, nil, nil
	case x509.SLHDSAWithSHAKE128f:
		return oids.OidSLHDSASHAKE_128f, nil, nil
	case x509.SLHDSAWithSHA192s:
		return oids.OidSLHDSASHA2_192s, nil, nil
	case x509.SLHDSAWithSHAKE192s:
		return oids.OidSLHDSASHAKE_192s, nil, nil
	case x509.SLHDSAWithSHA192f:
		return oids.OidSLHDSASHA2_192f, nil, nil
	case x509.SLHDSAWithSHAKE192f:
		return oids.OidSLHDSASHAKE_192f, nil, nil
	case x509.SLHDSAWithSHA256s:
		return oids.OidSLHDSASHA2_256s, nil, nil
	case x509.SLHDSAWithSHAKE256s:
		return oids.OidSLHDSASHAKE_256s, nil, nil
	case x509.SLHDSAWithSHA256f:
		return oids.OidSLHDSASHA2_256f, nil, nil
	case x509.SLHDSAWithSHAKE256f:
		return oids.OidSLHDSASHAKE_256f, nil, nil

	case x509.ECDSAWithSHAKE128:
		return oids.OidECDSAWithSHAKE128, nil, nil
	case x509.ECDSAWithSHAKE256:
		return oids.OidECDSAWithSHAKE256, nil, nil

	case x509.ECDSAWithSHA3256:
		return oids.OidECDSAWithSHA3_256, nil, nil
	case x509.ECDSAWithSHA3384:
		return oids.OidECDSAWithSHA3_384, nil, nil
	case x509.ECDSAWithSHA3512:
		return oids.OidECDSAWithSHA3_512, nil, nil

	case x509.SHAKE128WithRSAPSS:
		return oids.OidRSASSAPSSWithSHAKE128, nil, nil
	case x509.SHAKE256WithRSAPSS:
		return oids.OidRSASSAPSSWithSHAKE256, nil, nil

	case x509.SHA3256WithRSA:
		return oids.OidRSAPKCS1v15WithSHA3_256, nil, nil
	case x509.SHA3384WithRSA:
		return oids.OidRSAPKCS1v15WithSHA3_384, nil, nil
	case x509.SHA3512WithRSA:
		return oids.OidRSAPKCS1v15WithSHA3_512, nil, nil

	default:
		return nil, nil, oops.In(ServiceName).Errorf("no CMP OID mapping for signature algorithm: %v", sa)
	}
}

// ===== Reverse mapping: (OID, params) -> enum =====

func SigAlgFromAlgID(oid asn1.ObjectIdentifier, params *asn1.RawValue) (x509.SignatureAlgorithm, error) {
	switch {
	case oid.Equal(oids.OidMD5WithRSA):
		return x509.MD5WithRSA, nil
	case oid.Equal(oids.OidSignatureSHA1WithRSA):
		return x509.SHA1WithRSA, nil
	case oid.Equal(oids.OidSignatureSHA256WithRSA):
		return x509.SHA256WithRSA, nil
	case oid.Equal(oids.OidSignatureSHA384WithRSA):
		return x509.SHA384WithRSA, nil
	case oid.Equal(oids.OidSignatureSHA512WithRSA):
		return x509.SHA512WithRSA, nil

	case oid.Equal(oids.OidSignatureECDSAWithSHA1):
		return x509.ECDSAWithSHA1, nil
	case oid.Equal(oids.OidSignatureECDSAWithSHA256):
		return x509.ECDSAWithSHA256, nil
	case oid.Equal(oids.OidSignatureECDSAWithSHA384):
		return x509.ECDSAWithSHA384, nil
	case oid.Equal(oids.OidSignatureECDSAWithSHA512):
		return x509.ECDSAWithSHA512, nil

	case oid.Equal(oids.OidRSASSAPSS):
		hashOID, ok := parsePSSParams(params)
		if !ok {
			return x509.UnknownSignatureAlgorithm, oops.In(ServiceName).New("rsassaPss: missing/invalid parameters")
		}
		switch {
		case hashOID.Equal(oids.OidASN1SHA256):
			return x509.SHA256WithRSAPSS, nil
		case hashOID.Equal(oids.OidASN1SHA384):
			return x509.SHA384WithRSAPSS, nil
		case hashOID.Equal(oids.OidASN1SHA512):
			return x509.SHA512WithRSAPSS, nil
		default:
			return x509.UnknownSignatureAlgorithm, oops.In(ServiceName).Errorf("rsassaPss: unsupported hash OID %v", hashOID)
		}

	case oid.Equal(oids.OidEd25519):
		return x509.PureEd25519, nil
	case oid.Equal(oids.OidEd448):
		return x509.PureEd448, nil

	// SHAKE
	case oid.Equal(oids.OidRSASSAPSSWithSHAKE128):
		return x509.SHAKE128WithRSAPSS, nil
	case oid.Equal(oids.OidRSASSAPSSWithSHAKE256):
		return x509.SHAKE256WithRSAPSS, nil
	case oid.Equal(oids.OidECDSAWithSHAKE128):
		return x509.ECDSAWithSHAKE128, nil
	case oid.Equal(oids.OidECDSAWithSHAKE256):
		return x509.ECDSAWithSHAKE256, nil

	// SHA-3
	case oid.Equal(oids.OidECDSAWithSHA3_256):
		return x509.ECDSAWithSHA3256, nil
	case oid.Equal(oids.OidECDSAWithSHA3_384):
		return x509.ECDSAWithSHA3384, nil
	case oid.Equal(oids.OidECDSAWithSHA3_512):
		return x509.ECDSAWithSHA3512, nil

	case oid.Equal(oids.OidRSAPKCS1v15WithSHA3_256):
		return x509.SHA3256WithRSA, nil
	case oid.Equal(oids.OidRSAPKCS1v15WithSHA3_384):
		return x509.SHA3384WithRSA, nil
	case oid.Equal(oids.OidRSAPKCS1v15WithSHA3_512):
		return x509.SHA3512WithRSA, nil

	// ML-DSA
	case oid.Equal(oids.OidMLDSA44):
		return x509.PureMLDSA44, nil
	case oid.Equal(oids.OidMLDSA65):
		return x509.PureMLDSA65, nil
	case oid.Equal(oids.OidMLDSA87):
		return x509.PureMLDSA87, nil

	// Hash-SLH-DSA
	case oid.Equal(oids.OidSLHDSASHA2_128s):
		return x509.SLHDSAWithSHA128s, nil
	case oid.Equal(oids.OidSLHDSASHAKE_128s):
		return x509.SLHDSAWithSHAKE128s, nil
	case oid.Equal(oids.OidSLHDSASHA2_128f):
		return x509.SLHDSAWithSHA128f, nil
	case oid.Equal(oids.OidSLHDSASHAKE_128f):
		return x509.SLHDSAWithSHAKE128f, nil
	case oid.Equal(oids.OidSLHDSASHA2_192s):
		return x509.SLHDSAWithSHA192s, nil
	case oid.Equal(oids.OidSLHDSASHAKE_192s):
		return x509.SLHDSAWithSHAKE192s, nil
	case oid.Equal(oids.OidSLHDSASHA2_192f):
		return x509.SLHDSAWithSHA192f, nil
	case oid.Equal(oids.OidSLHDSASHAKE_192f):
		return x509.SLHDSAWithSHAKE192f, nil
	case oid.Equal(oids.OidSLHDSASHA2_256s):
		return x509.SLHDSAWithSHA256s, nil
	case oid.Equal(oids.OidSLHDSASHAKE_256s):
		return x509.SLHDSAWithSHAKE256s, nil
	case oid.Equal(oids.OidSLHDSASHA2_256f):
		return x509.SLHDSAWithSHA256f, nil
	case oid.Equal(oids.OidSLHDSASHAKE_256f):
		return x509.SLHDSAWithSHAKE256f, nil

	default:
		return x509.UnknownSignatureAlgorithm, oops.In(ServiceName).Errorf("unknown CMP signature OID: %v", oid)
	}
}

// GetAlgoAndHash returns the algorithm identifier and hash for the given key and OID.
// If oid is nil, returns the default algorithm for the signer's key
// Right now this is a bit overkill, since we currently only sign w/ rsa, but added ec for future support
func GetAlgoAndHash(pubKey crypto.PublicKey, oid *asn1.ObjectIdentifier) (pkix.AlgorithmIdentifier, crypto.Hash, error) {
	switch key := pubKey.(type) {
	case *rsa.PublicKey:
		switch {
		case oid == nil: // Default for RSA
			return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureSHA256WithRSA, Parameters: asn1.RawValue{Tag: asn1.TagNull}}, crypto.SHA256, nil
		case oid.Equal(oids.OidSignatureSHA1WithRSA):
			return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureSHA1WithRSA, Parameters: asn1.RawValue{Tag: asn1.TagNull}}, crypto.SHA1, nil
		case oid.Equal(oids.OidSignatureSHA256WithRSA):
			return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureSHA256WithRSA, Parameters: asn1.RawValue{Tag: asn1.TagNull}}, crypto.SHA256, nil
		case oid.Equal(oids.OidSignatureSHA384WithRSA):
			return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureSHA384WithRSA, Parameters: asn1.RawValue{Tag: asn1.TagNull}}, crypto.SHA384, nil
		case oid.Equal(oids.OidSignatureSHA512WithRSA):
			return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureSHA512WithRSA, Parameters: asn1.RawValue{Tag: asn1.TagNull}}, crypto.SHA512, nil
		default:
			return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Errorf("unsupported RSA signature algorithm: %s", oid.String())
		}

	case *ecdsa.PublicKey:
		// For ECDSA, check curve compatibility
		switch {
		case oid == nil: // Default based on curve
			switch key.Curve {
			case elliptic.P256():
				return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureECDSAWithSHA256}, crypto.SHA256, nil
			case elliptic.P384():
				return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureECDSAWithSHA384}, crypto.SHA384, nil
			case elliptic.P521():
				return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureECDSAWithSHA512}, crypto.SHA512, nil
			default:
				return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Errorf("unsupported ECDSA curve: %s", key.Curve.Params().Name)
			}
		case oid.Equal(oids.OidSignatureECDSAWithSHA1): // Sha 1 works with any curve
			return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureECDSAWithSHA1}, crypto.SHA1, nil
		case oid.Equal(oids.OidSignatureECDSAWithSHA256):
			if key.Curve == elliptic.P256() {
				return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureECDSAWithSHA256}, crypto.SHA256, nil
			}
			return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).New("ECDSA with SHA256 requires P256 curve")
		case oid.Equal(oids.OidSignatureECDSAWithSHA384):
			if key.Curve == elliptic.P384() {
				return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureECDSAWithSHA384}, crypto.SHA384, nil
			}
			return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).New("ECDSA with SHA384 requires P384 curve")
		case oid.Equal(oids.OidSignatureECDSAWithSHA512):
			if key.Curve == elliptic.P521() {
				return pkix.AlgorithmIdentifier{Algorithm: oids.OidSignatureECDSAWithSHA512}, crypto.SHA512, nil
			}
			return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).New("ECDSA with SHA512 requires P521 curve")
		default:
			return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Errorf("unsupported ECDSA signature algorithm: %s", oid.String())
		}
	case circl.PublicKey:
		defaultSigAlg := x509.SignatureAlgorithmByCirclScheme(key.Scheme())
		if defaultSigAlg == x509.UnknownSignatureAlgorithm {
			return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Errorf("unsupported Circl signature scheme: %s", key.Scheme().Name())
		}

		requestedSigAlg := defaultSigAlg
		if oid != nil {
			var err error
			requestedSigAlg, err = SigAlgFromAlgID(*oid, nil)
			if err != nil {
				return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Wrapf(err, "resolving requested Circl signature algorithm")
			}
			if requestedSigAlg != defaultSigAlg {
				return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Errorf(
					"requested signature algorithm %v does not match Circl key scheme %s",
					requestedSigAlg,
					key.Scheme().Name(),
				)
			}
		}

		algOID, params, err := AlgIDFromSigAlg(requestedSigAlg)
		if err != nil {
			return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Wrapf(err, "encoding Circl signature algorithm")
		}

		algID := pkix.AlgorithmIdentifier{Algorithm: algOID}
		if params != nil {
			algID.Parameters = *params
		}
		return algID, crypto.Hash(0), nil
	default:
		return getHashlessAlgoFromSPKI(pubKey, oid)
	}
}

func getHashlessAlgoFromSPKI(pubKey crypto.PublicKey, oid *asn1.ObjectIdentifier) (pkix.AlgorithmIdentifier, crypto.Hash, error) {
	pubDER, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Errorf("unsupported public key type: %T", pubKey)
	}

	var spki SubjectPublicKeyInfo
	if _, err := asn1.Unmarshal(pubDER, &spki); err != nil {
		return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Wrapf(err, "unmarshalling SubjectPublicKeyInfo")
	}

	algOID := spki.Algorithm.Algorithm
	params := &spki.Algorithm.Parameters
	if oid != nil {
		algOID = *oid
		params = nil
	}

	sigAlg, err := SigAlgFromAlgID(algOID, params)
	if err != nil {
		return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Wrapf(err, "resolving signature algorithm for public key type %T and OID %s", pubKey, algOID.String())
	}

	if !isHashlessSignatureAlgorithm(sigAlg) {
		return pkix.AlgorithmIdentifier{}, 0, oops.In(ServiceName).Errorf("signature algorithm %v for public key type %T is not hashless", sigAlg, pubKey)
	}

	algID := pkix.AlgorithmIdentifier{Algorithm: algOID}
	if oid == nil {
		algID.Parameters = spki.Algorithm.Parameters
	}
	return algID, crypto.Hash(0), nil
}

func isHashlessSignatureAlgorithm(sigAlg x509.SignatureAlgorithm) bool {
	switch sigAlg {
	case x509.PureEd25519,
		x509.PureEd448,
		x509.PureMLDSA44,
		x509.PureMLDSA65,
		x509.PureMLDSA87,
		x509.SLHDSAWithSHA128s,
		x509.SLHDSAWithSHAKE128s,
		x509.SLHDSAWithSHA128f,
		x509.SLHDSAWithSHAKE128f,
		x509.SLHDSAWithSHA192s,
		x509.SLHDSAWithSHAKE192s,
		x509.SLHDSAWithSHA192f,
		x509.SLHDSAWithSHAKE192f,
		x509.SLHDSAWithSHA256s,
		x509.SLHDSAWithSHAKE256s,
		x509.SLHDSAWithSHA256f,
		x509.SLHDSAWithSHAKE256f:
		return true
	default:
		return false
	}
}
