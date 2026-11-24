package cmp

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/asn1"
	"fmt"
	"io"

	"github.com/otpki/otpki-pkg/oids"
	"github.com/otpki/otpki-pkg/pkiutil"
	"golang.org/x/crypto/pbkdf2"
)

// PBMAC1Params is the PBMAC1-params structure from RFC 8018.
type PBMAC1Params struct {
	KeyDerivationFunc pkiutil.AlgorithmIdentifier
	MessageAuthScheme pkiutil.AlgorithmIdentifier
}

// PBKDF2Params supports the specified OCTET STRING salt form. KeyLength zero
// omits the optional field; an omitted PRF means HMAC-SHA-1 under RFC 8018.
type PBKDF2Params struct {
	Salt           asn1.RawValue
	IterationCount int
	KeyLength      int                         `asn1:"optional"`
	PRF            pkiutil.AlgorithmIdentifier `asn1:"optional"`
}

// MaxPBMAC1KeyLength bounds allocation and the number of PBKDF2 blocks. It
// accommodates keys up to the largest supported HMAC digest (SHA-512).
const MaxPBMAC1KeyLength = 64

// MaxPBMAC1Iterations permits common PBKDF2 profiles while bounding request work.
const MaxPBMAC1Iterations = 1_000_000

// NewPBMAC1Parameters generates independent PBKDF2/HMAC-SHA-256 parameters.
func NewPBMAC1Parameters(random io.Reader) (PBMAC1Params, error) {
	if random == nil {
		random = rand.Reader
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(random, salt); err != nil {
		return PBMAC1Params{}, fmt.Errorf("read PBMAC1 salt: %w", err)
	}
	mac := pkiutil.AlgorithmIdentifier{Algorithm: oids.OidHMACWithSHA256}
	kdf, err := asn1.Marshal(PBKDF2Params{
		Salt:           asn1.RawValue{Tag: asn1.TagOctetString, Bytes: salt},
		IterationCount: 10_000,
		KeyLength:      32,
		PRF:            mac,
	})
	if err != nil {
		return PBMAC1Params{}, fmt.Errorf("marshal PBKDF2 parameters: %w", err)
	}
	return PBMAC1Params{
		KeyDerivationFunc: pkiutil.AlgorithmIdentifier{
			Algorithm:  oids.OidPBKDF2,
			Parameters: asn1.RawValue{FullBytes: kdf},
		},
		MessageAuthScheme: mac,
	}, nil
}

// ComputePBMAC1 validates work parameters before deriving a key or allocating
// key material. An omitted keyLength uses the message MAC's digest size.
func ComputePBMAC1(secret, data []byte, parameters PBMAC1Params, allowSHA1 bool) ([]byte, error) {
	if !parameters.KeyDerivationFunc.Algorithm.Equal(oids.OidPBKDF2) {
		return nil, fmt.Errorf("unsupported PBMAC1 key derivation function")
	}
	var kdf PBKDF2Params
	der := parameters.KeyDerivationFunc.Parameters.FullBytes
	rest, err := asn1.Unmarshal(der, &kdf)
	if err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("invalid PBKDF2 parameters")
	}
	if kdf.IterationCount <= 0 || kdf.IterationCount > MaxPBMAC1Iterations {
		return nil, fmt.Errorf("invalid PBKDF2 iterationCount %d", kdf.IterationCount)
	}
	if kdf.Salt.Class != asn1.ClassUniversal || kdf.Salt.Tag != asn1.TagOctetString || kdf.Salt.IsCompound {
		return nil, fmt.Errorf("unsupported PBKDF2 salt form")
	}
	if len(kdf.Salt.Bytes) < 8 || len(kdf.Salt.Bytes) > 1024 {
		return nil, fmt.Errorf("invalid PBKDF2 salt length")
	}
	prfOID := kdf.PRF.Algorithm
	if len(prfOID) == 0 {
		prfOID = oids.OidHMACWithSHA1_PKCS
	}
	prf, err := MACFromOID(prfOID, allowSHA1)
	if err != nil {
		return nil, fmt.Errorf("PBKDF2 PRF: %w", err)
	}
	mac, err := MACFromOID(parameters.MessageAuthScheme.Algorithm, allowSHA1)
	if err != nil {
		return nil, fmt.Errorf("PBMAC1 MAC: %w", err)
	}
	keyLength := kdf.KeyLength
	if keyLength == 0 {
		keyLength = mac().Size()
	}
	if keyLength <= 0 || keyLength > MaxPBMAC1KeyLength {
		return nil, fmt.Errorf("invalid PBKDF2 keyLength %d", kdf.KeyLength)
	}
	key := pbkdf2.Key(secret, kdf.Salt.Bytes, kdf.IterationCount, keyLength, prf)
	h := hmac.New(mac, key)
	_, _ = h.Write(data)
	return h.Sum(nil), nil
}
