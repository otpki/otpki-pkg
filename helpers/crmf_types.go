package helpers

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
)

// AttributeTypeAndValue ::= SEQUENCE { type OBJECT IDENTIFIER, value ANY DEFINED BY type }
type AttributeTypeAndValue struct {
	Type  asn1.ObjectIdentifier
	Value asn1.RawValue
}

// CertTemplate ::= SEQUENCE { ... }
// Only the fields you need for now.
type CertTemplate struct {
	Version      int                  `asn1:"optional,tag:0"`
	SerialNumber *big.Int             `asn1:"optional,tag:1"`
	Issuer       asn1.RawValue        `asn1:"optional,tag:3"`
	Subject      asn1.RawValue        `asn1:"optional,tag:5"` // pkix.Name-encoded DER (Name)
	PublicKey    SubjectPublicKeyInfo `asn1:"optional,tag:6"`
	Extensions   []pkix.Extension     `asn1:"optional,tag:9,sequence"`
}

// CertRequest ::= SEQUENCE { certReqId INTEGER, certTemplate CertTemplate, controls OPTIONAL }
type CertRequest struct {
	CertReqID    int
	CertTemplate CertTemplate
	// Controls omitted for now
}

type CertReqMsg struct {
	CertReq CertRequest
	Popo    asn1.RawValue `asn1:"optional"`
	// RegInfo not needed yet
	// RegInfo []AttributeTypeAndValue `asn1:"optional,sequence"`

	// Not part of ASN.1; set by decoder so POP verification uses exact bytes.
	RawCertReqDER []byte `asn1:"-"`
}

// for decoding only
type certReqMsgWire struct {
	CertReq asn1.RawValue
	Popo    asn1.RawValue `asn1:"optional"` // raw CHOICE wrapper stays here
	RegInfo asn1.RawValue `asn1:"optional"`
}

func UnmarshalCertReqMessagesWithRaw(der []byte) (CertReqMessages, error) {
	// Outer is SEQUENCE OF CertReqMsg; easiest decode is into []RawValue.
	var elems []asn1.RawValue
	if _, err := asn1.Unmarshal(der, &elems); err != nil {
		return nil, err
	}
	out := make(CertReqMessages, 0, len(elems))
	for _, e := range elems {
		var w certReqMsgWire
		if _, err := asn1.Unmarshal(e.FullBytes, &w); err != nil {
			return nil, err
		}

		var cr CertRequest
		if _, err := asn1.Unmarshal(w.CertReq.FullBytes, &cr); err != nil {
			return nil, err
		}

		rm := CertReqMsg{
			CertReq:       cr,
			Popo:          w.Popo,
			RawCertReqDER: append([]byte(nil), w.CertReq.FullBytes...),
		}
		out = append(out, rm)
	}
	return out, nil
}

// CertReqMessages ::= SEQUENCE SIZE (1..MAX) OF CertReqMsg
type CertReqMessages []CertReqMsg

func MarshalCertReqMsgDER(m CertReqMsg) ([]byte, error) {
	return asn1.Marshal(m)
}

func UnmarshalCertReqMsgDER(der []byte) (CertReqMsg, error) {
	var m CertReqMsg
	_, err := asn1.Unmarshal(der, &m)
	return m, err
}
