package cmp

import (
	"encoding/asn1"
	"fmt"
	"slices"

	"github.com/otpki/iogo/crypto/x509"

	"github.com/otpki/otpki-pkg/crmf"
	"github.com/otpki/otpki-pkg/pkiutil"
)

func DecodePKIMessage(der []byte) (*PKIMessage, asn1.RawValue, asn1.RawValue, error) {
	var raw struct {
		Header     PKIHeader
		Body       asn1.RawValue
		Protection asn1.BitString  `asn1:"tag:0,optional,explicit"`
		ExtraCerts []asn1.RawValue `asn1:"tag:1,optional,explicit,sequence"`
	}

	var top asn1.RawValue
	if _, err := asn1.Unmarshal(der, &top); err != nil {
		return nil, asn1.RawValue{}, asn1.RawValue{}, err
	}
	rest := top.Bytes

	var hdrRV asn1.RawValue
	rest, err := asn1.Unmarshal(rest, &hdrRV)
	if err != nil {
		return nil, asn1.RawValue{}, asn1.RawValue{}, err
	}

	var bodyRV asn1.RawValue
	if _, err = asn1.Unmarshal(rest, &bodyRV); err != nil {
		return nil, asn1.RawValue{}, asn1.RawValue{}, err
	}

	if _, err := asn1.Unmarshal(der, &raw); err != nil {
		return nil, asn1.RawValue{}, asn1.RawValue{}, err
	}

	body, err := decodePKIBody(raw.Body)
	if err != nil {
		return nil, asn1.RawValue{}, asn1.RawValue{}, err
	}
	body.Raw = raw.Body

	cmpCerts := make([]CMPCertificate, 0, len(raw.ExtraCerts))
	for _, rv := range raw.ExtraCerts {
		c := CMPCertificate{Raw: rv.FullBytes}
		if cert, err := x509.ParseCertificate(rv.FullBytes); err == nil {
			c.Certificate = cert
		}
		cmpCerts = append(cmpCerts, c)
	}

	msg := &PKIMessage{
		Header:         raw.Header,
		Body:           body,
		ExtraCerts:     cmpCerts,
		WireMessageDER: slices.Clone(der),
		WireHeaderDER:  hdrRV.FullBytes,
		WireBodyDER:    bodyRV.FullBytes,
	}

	if len(raw.Protection.Bytes) != 0 || raw.Protection.BitLength != 0 {
		msg.Protection = &raw.Protection
	}

	return msg, hdrRV, bodyRV, nil
}

func decodePKIBody(raw asn1.RawValue) (PKIBody, error) {
	if raw.Class != asn1.ClassContextSpecific {
		return PKIBody{}, fmt.Errorf("bad ASN.1 class for PKIBody: %d", raw.Class)
	}

	bodyType := PKIBodyType(raw.Tag)
	body := PKIBody{Type: bodyType}

	switch bodyType {
	case BodyTypeIR, BodyTypeCR, BodyTypeKUR:
		inner := pkiutil.EnsureDERSequence(raw.Bytes)
		crm, err := crmf.UnmarshalCertReqMessagesWithRaw(inner)
		if err != nil {
			return PKIBody{}, fmt.Errorf("decoding %s CertReqMessages: %w", bodyType, err)
		}
		body.Value = crm
	case BodyTypeIP, BodyTypeCP, BodyTypeKUP:
		inner := pkiutil.EnsureDERSequence(raw.Bytes)
		var rep CertRepMessage
		if _, err := asn1.Unmarshal(inner, &rep); err != nil {
			return PKIBody{}, fmt.Errorf("decoding %s CertRepMessage: %w", bodyType, err)
		}
		body.Value = rep
	case BodyTypeP10CR:
		body.Value = pkiutil.EnsureDERSequence(raw.Bytes)
	case BodyTypeRR:
		var rr RevReqContent
		inner := pkiutil.EnsureDERSequence(raw.Bytes)
		if _, err := asn1.Unmarshal(inner, &rr); err != nil {
			return PKIBody{}, fmt.Errorf("decoding rr RevReqContent: %w", err)
		}
		body.Value = rr
	case BodyTypeCertConf:
		var cc CertConfirmContent
		inner := pkiutil.EnsureDERSequence(raw.Bytes)
		if _, err := asn1.Unmarshal(inner, &cc); err != nil {
			return PKIBody{}, fmt.Errorf("decoding certConf CertConfirmContent: %w", err)
		}
		body.Value = cc
	case BodyTypePollReq:
		var prc PollReqContent
		inner := pkiutil.EnsureDERSequence(raw.Bytes)
		if _, err := asn1.Unmarshal(inner, &prc); err != nil {
			return PKIBody{}, fmt.Errorf("decoding pollReq PollReqContent: %w", err)
		}
		body.Value = prc
	case BodyTypePollRep:
		var prc PollRepContent
		inner := pkiutil.EnsureDERSequence(raw.Bytes)
		if _, err := asn1.Unmarshal(inner, &prc); err != nil {
			return PKIBody{}, fmt.Errorf("decoding pollRep PollRepContent: %w", err)
		}
		body.Value = prc
	case BodyTypePKIConf:
		if string(raw.Bytes) != string(asn1.NullBytes) {
			return PKIBody{}, fmt.Errorf("invalid pkiConf")
		}
		body.Value = asn1.NullRawValue
	case BodyTypeGenM, BodyTypeGenP:
		var values GenMsgContent
		tail, err := asn1.Unmarshal(raw.Bytes, &values)
		if err != nil || len(tail) != 0 {
			return PKIBody{}, fmt.Errorf("invalid general message")
		}
		body.Value = values
	case BodyTypeError:
		var emc ErrorMsgContent
		inner := pkiutil.EnsureDERSequence(raw.Bytes)
		if _, err := asn1.Unmarshal(inner, &emc); err != nil {
			return PKIBody{}, fmt.Errorf("decoding error ErrorMsgContent: %w", err)
		}
		body.Value = emc
	default:
		return PKIBody{}, fmt.Errorf("unsupported PKIBody tag: %d", raw.Tag)
	}

	return body, nil
}

func EncodeBodyRaw(msg *PKIMessage) (asn1.RawValue, error) {
	if msg == nil {
		return asn1.RawValue{}, fmt.Errorf("encoding PKIBody requires a message")
	}

	switch msg.Body.Type {
	case BodyTypeIR, BodyTypeCR, BodyTypeKUR:
		crm, ok := msg.Body.Value.(crmf.CertReqMessages)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("%s: expected CertReqMessages, got %T", msg.Body.Type, msg.Body.Value)
		}
		inner, err := asn1.Marshal(crm)
		if err != nil {
			return asn1.RawValue{}, fmt.Errorf("marshaling %s CertReqMessages: %w", msg.Body.Type, err)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      inner,
		}, nil
	case BodyTypeP10CR:
		der, ok := msg.Body.Value.([]byte)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("p10cr: expected []byte, got %T", msg.Body.Value)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      der,
		}, nil
	case BodyTypeRR:
		rr, ok := msg.Body.Value.(RevReqContent)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("rr: expected RevReqContent, got %T", msg.Body.Value)
		}
		inner, err := asn1.Marshal(rr)
		if err != nil {
			return asn1.RawValue{}, fmt.Errorf("marshaling rr RevReqContent: %w", err)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      inner,
		}, nil
	case BodyTypeCertConf:
		cc, ok := msg.Body.Value.(CertConfirmContent)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("certConf: expected CertConfirmContent, got %T", msg.Body.Value)
		}
		inner, err := asn1.Marshal(cc)
		if err != nil {
			return asn1.RawValue{}, fmt.Errorf("marshaling certConf CertConfirmContent: %w", err)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      inner,
		}, nil
	case BodyTypePollReq:
		prc, ok := msg.Body.Value.(PollReqContent)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("pollReq: expected PollReqContent, got %T", msg.Body.Value)
		}
		inner, err := asn1.Marshal(prc)
		if err != nil {
			return asn1.RawValue{}, fmt.Errorf("marshaling pollReq PollReqContent: %w", err)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      inner,
		}, nil
	case BodyTypePollRep:
		prc, ok := msg.Body.Value.(PollRepContent)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("pollRep: expected PollRepContent, got %T", msg.Body.Value)
		}
		inner, err := asn1.Marshal(prc)
		if err != nil {
			return asn1.RawValue{}, fmt.Errorf("marshaling pollRep PollRepContent: %w", err)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      inner,
		}, nil
	case BodyTypeIP, BodyTypeCP, BodyTypeKUP:
		crm, ok := msg.Body.Value.(CertRepMessage)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("%s: expected CertRepMessage, got %T", msg.Body.Type, msg.Body.Value)
		}
		inner, err := asn1.Marshal(crm)
		if err != nil {
			return asn1.RawValue{}, fmt.Errorf("marshaling %s CertRepMessage: %w", msg.Body.Type, err)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      inner,
		}, nil
	case BodyTypeRP:
		rp, ok := msg.Body.Value.(RevRepContent)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("rp: expected RevRepContent, got %T", msg.Body.Value)
		}
		inner, err := asn1.Marshal(rp)
		if err != nil {
			return asn1.RawValue{}, fmt.Errorf("marshaling rp RevRepContent: %w", err)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      inner,
		}, nil
	case BodyTypePKIConf:
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      asn1.NullBytes,
		}, nil
	case BodyTypeGenM, BodyTypeGenP:
		values, ok := msg.Body.Value.(GenMsgContent)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("expected general message")
		}
		der, err := asn1.Marshal(values)
		if err != nil {
			return asn1.RawValue{}, err
		}
		return asn1.RawValue{Class: 2, Tag: int(msg.Body.Type), IsCompound: true, Bytes: der}, nil
	case BodyTypeError:
		emc, ok := msg.Body.Value.(ErrorMsgContent)
		if !ok {
			return asn1.RawValue{}, fmt.Errorf("error: expected ErrorMsgContent, got %T", msg.Body.Value)
		}
		inner, err := asn1.Marshal(emc)
		if err != nil {
			return asn1.RawValue{}, fmt.Errorf("marshaling error ErrorMsgContent: %w", err)
		}
		return asn1.RawValue{
			Class:      asn1.ClassContextSpecific,
			Tag:        int(msg.Body.Type),
			IsCompound: true,
			Bytes:      inner,
		}, nil
	default:
		return asn1.RawValue{}, fmt.Errorf("encoding PKIBody does not support body type %s", msg.Body.Type)
	}
}

func MarshalPKIFreeText(ft PKIFreeText) (asn1.RawValue, error) {
	items := make([]asn1.RawValue, 0, len(ft))

	for _, s := range ft {
		der, err := asn1.MarshalWithParams(string(s), "utf8")
		if err != nil {
			return asn1.RawValue{}, err
		}

		var rv asn1.RawValue
		if _, err := asn1.Unmarshal(der, &rv); err != nil {
			return asn1.RawValue{}, err
		}
		items = append(items, rv)
	}

	seqDER, err := asn1.Marshal(items)
	if err != nil {
		return asn1.RawValue{}, err
	}

	var seqRV asn1.RawValue
	if _, err := asn1.Unmarshal(seqDER, &seqRV); err != nil {
		return asn1.RawValue{}, err
	}
	return seqRV, nil
}
