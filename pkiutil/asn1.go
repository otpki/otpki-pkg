package pkiutil

import (
	"encoding/asn1"
)

const DerSequence = asn1.TagSequence | 0x20

// AlgorithmIdentifier matches X.509 / RFC 5280 and is used all over CMP+CRMF.
type AlgorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

// SubjectPublicKeyInfo matches X.509 SPKI.
type SubjectPublicKeyInfo struct {
	Algorithm        AlgorithmIdentifier
	SubjectPublicKey asn1.BitString
}

func (s SubjectPublicKeyInfo) DER() ([]byte, error) {
	// This marshals the *entire* SubjectPublicKeyInfo SEQUENCE
	// (AlgorithmIdentifier + BIT STRING)
	return asn1.Marshal(s)
}

func derWrapSequence(content []byte) []byte {
	n := len(content)
	// DER length encoding
	var lenBytes []byte
	if n < 128 {
		lenBytes = []byte{byte(n)}
	} else {
		tmp := make([]byte, 0, 8)
		var lengthOctets byte
		for x := n; x > 0; x >>= 8 {
			lengthOctets++
			tmp = append([]byte{byte(x & 0xff)}, tmp...)
		}
		lenBytes = append([]byte{0x80 | lengthOctets}, tmp...)
	}

	out := make([]byte, 0, 1+len(lenBytes)+n)
	out = append(out, DerSequence) // SEQUENCE
	out = append(out, lenBytes...) // length
	out = append(out, content...)  // contents
	return out
}

func EnsureDERSequence(maybeContent []byte) []byte {
	if len(maybeContent) == 0 {
		return maybeContent
	}
	// If it already starts with SEQUENCE, assume it is a full DER SEQUENCE.
	if maybeContent[0] == DerSequence {
		return maybeContent
	}
	// Otherwise it’s the contents of an IMPLICIT-tagged SEQUENCE; wrap it.
	return derWrapSequence(maybeContent)
}

func ASN1BitLength(bitString []byte) int {
	bitLen := len(bitString) * 8

	for i := range bitString {
		b := bitString[len(bitString)-i-1]

		for bit := range uint(8) {
			if (b>>bit)&1 == 1 {
				return bitLen
			}
			bitLen--
		}
	}

	return 0
}

func ReverseBitsInAByte(in byte) byte {
	b1 := in>>4 | in<<4
	b2 := b1>>2&0x33 | b1<<2&0xcc
	b3 := b2>>1&0x55 | b2<<1&0xaa
	return b3
}
