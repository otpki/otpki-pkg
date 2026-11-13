package crmf

import (
	"crypto"
	"crypto/rand"
	"encoding/asn1"
	"fmt"
	"io"

	"github.com/otpki/iogo/crypto/x509"
	"github.com/otpki/otpki-pkg/pkiutil"
)

type POPOSigningKey struct {
	PoposkInput asn1.RawValue `asn1:"optional,tag:0"` // IMPLICIT [0]
	AlgID       pkiutil.AlgorithmIdentifier
	Signature   asn1.BitString
}

func ParsePOPOSigningKey(content []byte) (POPOSigningKey, error) {
	var out POPOSigningKey
	rest := content

	// Optional poposkInput [0]
	if len(rest) > 0 && rest[0] == 0xA0 {
		var rv asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &rv); err != nil {
			return out, fmt.Errorf("decoding poposkInput: %w", err)
		}
		out.PoposkInput = rv // keep raw; you decide what bytes to sign later
	}

	// algorithmIdentifier
	alg, rest2, err := parseAlgorithmIdentifierPossiblyContents(rest)
	if err != nil {
		return out, fmt.Errorf("decoding algorithm identifier: %w", err)
	}
	out.AlgID = alg
	rest = rest2

	// signature BIT STRING
	var sig asn1.BitString
	if _, err := asn1.Unmarshal(rest, &sig); err != nil {
		return out, fmt.Errorf("decoding POP signature: %w", err)
	}
	out.Signature = sig
	return out, nil
}

// Accept either a full DER AlgorithmIdentifier SEQUENCE (0x30...)
// or just its contents starting with OBJECT IDENTIFIER (0x06...).
func parseAlgorithmIdentifierPossiblyContents(in []byte) (pkiutil.AlgorithmIdentifier, []byte, error) {
	if len(in) == 0 {
		return pkiutil.AlgorithmIdentifier{}, nil, fmt.Errorf("algorithm identifier input is empty")
	}

	// Normal case: SEQUENCE
	if in[0] == pkiutil.DerSequence {
		var alg pkiutil.AlgorithmIdentifier
		rest, err := asn1.Unmarshal(in, &alg)
		return alg, rest, err
	}

	// Contents case: starts with OID (0x06)
	if in[0] == asn1.TagOID {
		var oid asn1.ObjectIdentifier
		rest, err := asn1.Unmarshal(in, &oid)
		if err != nil {
			return pkiutil.AlgorithmIdentifier{}, nil, err
		}

		alg := pkiutil.AlgorithmIdentifier{Algorithm: oid}

		// Optional params: if next is not BIT STRING (0x03), treat it as params value.
		if len(rest) > 0 && rest[0] != asn1.TagBitString {
			var params asn1.RawValue
			rest2, err := asn1.Unmarshal(rest, &params)
			if err != nil {
				return pkiutil.AlgorithmIdentifier{}, nil, fmt.Errorf("decoding algorithm identifier parameters: %w", err)
			}
			alg.Parameters = params
			rest = rest2
		}

		return alg, rest, nil
	}
	return pkiutil.AlgorithmIdentifier{}, nil, fmt.Errorf("unexpected algorithm identifier tag 0x%02X", in[0])
}

// SignatureInput returns the exact signed bytes for CRMF signature POP.
// Callers decide whether reconstructing an absent certReqDER is acceptable.
func SignatureInput(certReqDER []byte, input asn1.RawValue) ([]byte, error) {
	if len(input.FullBytes) != 0 || len(input.Bytes) != 0 {
		return asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: input.Bytes})
	}
	if len(certReqDER) == 0 {
		return nil, fmt.Errorf("CRMF certReq bytes unavailable for signature POP")
	}
	return append([]byte(nil), certReqDER...), nil
}

// PublicKey extracts the certified public key without making a trust decision.
func PublicKey(req CertRequest) (crypto.PublicKey, error) {
	spki := req.CertTemplate.PublicKey
	if len(spki.Algorithm.Algorithm) == 0 {
		return nil, fmt.Errorf("subjectPublicKeyInfo.algorithm is missing")
	}
	if len(spki.SubjectPublicKey.Bytes) == 0 || spki.SubjectPublicKey.BitLength == 0 {
		return nil, fmt.Errorf("subjectPublicKeyInfo.subjectPublicKey is missing")
	}
	der, err := asn1.Marshal(spki)
	if err != nil {
		return nil, fmt.Errorf("marshaling SubjectPublicKeyInfo: %w", err)
	}
	return x509.ParsePKIXPublicKey(der)
}

// BuildSignaturePOP signs the DER CertRequest; authorization is the caller's policy.
func BuildSignaturePOP(certReqDER []byte, key crypto.Signer, random io.Reader) (asn1.RawValue, error) {
	if len(certReqDER) == 0 {
		return asn1.RawValue{}, fmt.Errorf("empty CertRequest DER")
	}
	if key == nil {
		return asn1.RawValue{}, fmt.Errorf("missing signer key")
	}
	if random == nil {
		random = rand.Reader
	}
	alg, hash, err := pkiutil.SignerAlgorithmFromPublicKey(key.Public())
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("resolve POP algorithm: %w", err)
	}
	sig, err := crypto.SignMessage(key, random, certReqDER, hash)
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("sign cert request POP: %w", err)
	}
	algDER, err := asn1.Marshal(alg)
	if err != nil {
		return asn1.RawValue{}, err
	}
	sigDER, err := asn1.Marshal(asn1.BitString{Bytes: sig, BitLength: len(sig) * 8})
	if err != nil {
		return asn1.RawValue{}, err
	}
	return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 1, IsCompound: true, Bytes: pkiutil.EnsureDERSequence(append(algDER, sigDER...))}, nil
}
