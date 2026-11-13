package cmp

import (
	"encoding/asn1"
	"fmt"
)

func MarshalPKIMessage(msg *PKIMessage) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("marshaling PKI message requires a message")
	}

	bodyRaw, err := EncodeBodyRaw(msg)
	if err != nil {
		return nil, err
	}

	extra := make([]asn1.RawValue, 0, len(msg.ExtraCerts))
	for _, c := range msg.ExtraCerts {
		if len(c.Raw) != 0 {
			extra = append(extra, asn1.RawValue{FullBytes: c.Raw})
		} else if c.Certificate != nil {
			extra = append(extra, asn1.RawValue{FullBytes: c.Certificate.Raw})
		}
	}

	var wrapper struct {
		Header     PKIHeader
		Body       asn1.RawValue
		Protection asn1.BitString  `asn1:"tag:0,optional,explicit"`
		ExtraCerts []asn1.RawValue `asn1:"tag:1,optional,explicit,sequence"`
	}
	wrapper.Header = msg.Header
	wrapper.Body = bodyRaw
	wrapper.ExtraCerts = extra
	if msg.Protection != nil && (len(msg.Protection.Bytes) != 0 || msg.Protection.BitLength != 0) {
		wrapper.Protection = *msg.Protection
	}

	// ----- Encode whole PKIMessage -----
	return asn1.Marshal(wrapper)
}

// encodeProtectedPart encodes SEQUENCE { header PKIHeader, body PKIBody }
// Needed to protect with signature/PBMAC
func MarshalProtectedPart(msg *PKIMessage) ([]byte, error) {
	bodyRaw, err := EncodeBodyRaw(msg)
	if err != nil {
		return nil, err
	}

	var pp struct {
		Header PKIHeader
		Body   asn1.RawValue
	}
	pp.Header = msg.Header
	pp.Body = bodyRaw

	return asn1.Marshal(pp)
}

// protectedPartFromMsgWire recreates protected part from wire to verify signature/PBMAC
func ProtectedPartFromWire(msg *PKIMessage) ([]byte, error) {
	if msg == nil || len(msg.WireHeaderDER) == 0 || len(msg.WireBodyDER) == 0 {
		return nil, fmt.Errorf("protected part requires wire header and body bytes")
	}
	pp := asn1.RawValue{
		Class:      asn1.ClassUniversal,
		Tag:        asn1.TagSequence,
		IsCompound: true,
		Bytes:      append(append([]byte{}, msg.WireHeaderDER...), msg.WireBodyDER...),
	}
	return asn1.Marshal(pp)
}
