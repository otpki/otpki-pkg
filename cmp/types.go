package cmp

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"

	"github.com/otpki/iogo/crypto/x509"

	"github.com/otpki/otpki-pkg/pkiutil"
)

type (
	PKIBodyType int
	UTF8String  string
)

const (
	BodyTypeIR       PKIBodyType = 0
	BodyTypeIP       PKIBodyType = 1
	BodyTypeCR       PKIBodyType = 2
	BodyTypeCP       PKIBodyType = 3
	BodyTypeP10CR    PKIBodyType = 4
	BodyTypeKUR      PKIBodyType = 7
	BodyTypeKUP      PKIBodyType = 8
	BodyTypeRR       PKIBodyType = 11
	BodyTypeRP       PKIBodyType = 12
	BodyTypePKIConf  PKIBodyType = 19
	BodyTypeGenM     PKIBodyType = 21
	BodyTypeGenP     PKIBodyType = 22
	BodyTypeError    PKIBodyType = 23
	BodyTypeCertConf PKIBodyType = 24
	BodyTypePollReq  PKIBodyType = 25
	BodyTypePollRep  PKIBodyType = 26
)

func (t PKIBodyType) String() string {
	switch t {
	case BodyTypeIR:
		return "ir"
	case BodyTypeIP:
		return "ip"
	case BodyTypeCR:
		return "cr"
	case BodyTypeCP:
		return "cp"
	case BodyTypeP10CR:
		return "p10cr"
	case BodyTypeKUR:
		return "kur"
	case BodyTypeKUP:
		return "kup"
	case BodyTypeRR:
		return "rr"
	case BodyTypeRP:
		return "rp"
	case BodyTypePKIConf:
		return "pkiConf"
	case BodyTypeGenM:
		return "genm"
	case BodyTypeGenP:
		return "genp"
	case BodyTypeError:
		return "error"
	case BodyTypeCertConf:
		return "certConf"
	case BodyTypePollReq:
		return "pollReq"
	case BodyTypePollRep:
		return "pollRep"
	default:
		return "unknown"
	}
}

// InfoTypeAndValue carries an OID-selected optional value, GenMsgContent
// groups these for general queries and replies (RFC 9480, Appendix A.1).
type InfoTypeAndValue struct {
	InfoType  asn1.ObjectIdentifier
	InfoValue asn1.RawValue `asn1:"optional"`
}
type GenMsgContent []InfoTypeAndValue

type CertConfirmContent []CertStatus

type CertStatus struct {
	CertHash  []byte
	CertReqID int
	Status    asn1.RawValue `asn1:"optional"`
}

type PollReqContent []PollReq

type PollReq struct {
	CertReqID int
}

type PollRepContent []PollRep

type PollRep struct {
	CertReqID  int
	CheckAfter int
	Reason     asn1.RawValue `asn1:"optional"`
}

type PKIFreeText []UTF8String

func FreeText(ss ...string) PKIFreeText {
	out := make(PKIFreeText, 0, len(ss))
	for _, s := range ss {
		out = append(out, UTF8String(s))
	}
	return out
}

type PKIStatusInfo struct {
	Status       int
	StatusString asn1.RawValue  `asn1:"optional"`
	FailInfo     asn1.BitString `asn1:"optional"`
}

type CMPCertificate struct { //nolint:recvcheck,revive // Preserve the standard ASN.1 name and value/pointer marshaling API.
	Raw         []byte
	Certificate *x509.Certificate
}

func (c CMPCertificate) MarshalASN1() ([]byte, error) {
	if len(c.Raw) != 0 {
		return c.Raw, nil
	}
	if c.Certificate != nil && len(c.Certificate.Raw) != 0 {
		return c.Certificate.Raw, nil
	}
	return nil, errors.New("CMPCertificate: empty")
}

func (c *CMPCertificate) UnmarshalASN1(b []byte) error {
	if c == nil {
		return errors.New("CMPCertificate: nil receiver")
	}
	c.Raw = append([]byte(nil), b...)
	if cert, err := x509.ParseCertificate(b); err == nil {
		c.Certificate = cert
	} else {
		c.Certificate = nil
	}
	return nil
}

type PKIHeader struct {
	Pvno          int
	Sender        asn1.RawValue
	Recipient     asn1.RawValue
	MessageTime   asn1.RawValue      `asn1:"tag:0,optional,explicit"`
	ProtectionAlg asn1.RawValue      `asn1:"tag:1,optional"`
	SenderKID     []byte             `asn1:"tag:2,optional,explicit"`
	RecipKID      []byte             `asn1:"tag:3,optional,explicit"`
	TransactionID []byte             `asn1:"tag:4,optional,explicit"`
	SenderNonce   []byte             `asn1:"tag:5,optional,explicit"`
	RecipNonce    []byte             `asn1:"tag:6,optional,explicit"`
	FreeText      asn1.RawValue      `asn1:"tag:7,optional,explicit"`
	GeneralInfo   []InfoTypeAndValue `asn1:"tag:8,optional,explicit,sequence"`
}

func (h *PKIHeader) ParsedProtectionAlg() (*pkiutil.AlgorithmIdentifier, bool, error) {
	if len(h.ProtectionAlg.FullBytes) == 0 && len(h.ProtectionAlg.Bytes) == 0 {
		return nil, false, nil
	}

	if h.ProtectionAlg.Class == asn1.ClassContextSpecific && h.ProtectionAlg.Tag == 1 && len(h.ProtectionAlg.Bytes) != 0 {
		var alg pkiutil.AlgorithmIdentifier
		if _, err := asn1.Unmarshal(h.ProtectionAlg.Bytes, &alg); err != nil {
			return nil, false, err
		}
		return &alg, true, nil
	}

	if len(h.ProtectionAlg.FullBytes) != 0 && h.ProtectionAlg.FullBytes[0] == pkiutil.DerSequence {
		var alg pkiutil.AlgorithmIdentifier
		if _, err := asn1.Unmarshal(h.ProtectionAlg.FullBytes, &alg); err != nil {
			return nil, false, err
		}
		return &alg, true, nil
	}

	if len(h.ProtectionAlg.FullBytes) != 0 && h.ProtectionAlg.FullBytes[0] == 0xA1 {
		var outer asn1.RawValue
		if _, err := asn1.Unmarshal(h.ProtectionAlg.FullBytes, &outer); err != nil {
			return nil, false, err
		}
		if outer.Class == asn1.ClassContextSpecific && outer.Tag == 1 {
			var alg pkiutil.AlgorithmIdentifier
			if _, err := asn1.Unmarshal(outer.Bytes, &alg); err != nil {
				return nil, false, err
			}
			return &alg, true, nil
		}
	}
	return nil, false, fmt.Errorf("unexpected ProtectionAlg encoding")
}

func (h *PKIHeader) SetProtectionAlg(alg pkiutil.AlgorithmIdentifier) error {
	innerDER, err := asn1.Marshal(alg)
	if err != nil {
		return err
	}

	h.ProtectionAlg = asn1.RawValue{
		Class:      asn1.ClassContextSpecific,
		Tag:        1,
		IsCompound: true,
		Bytes:      innerDER,
	}
	return nil
}

type PKIBody struct {
	Type  PKIBodyType
	Value any
	Raw   asn1.RawValue
}

type PKIMessage struct {
	Header     PKIHeader
	Body       PKIBody
	Protection *asn1.BitString
	ExtraCerts []CMPCertificate

	WireMessageDER []byte
	WireHeaderDER  []byte
	WireBodyDER    []byte
}

type RequestContext struct {
	Msg *PKIMessage
}

type CertRepMessage struct {
	CAPubs    []asn1.RawValue `asn1:"tag:1,optional,explicit,sequence"`
	Responses []CertResponse  `asn1:"sequence"`
}

type CertResponse struct {
	CertReqID        int
	Status           PKIStatusInfo
	CertifiedKeyPair CertifiedKeyPair `asn1:"optional"`
	RspInfo          []byte           `asn1:"optional"`
}

type CertifiedKeyPair struct {
	CertOrEncCert asn1.RawValue
}

type RevReqContent struct {
	Details []RevDetails `asn1:"sequence"`
}

type RevDetails struct {
	CertDetails     asn1.RawValue
	CRLEntryDetails []pkix.Extension `asn1:"optional,sequence"`
}

type RevRepContent struct {
	Status   []PKIStatusInfo  `asn1:"sequence"`
	RevCerts []CMPCertificate `asn1:"optional,tag:0,explicit,sequence"`
	CRLs     []asn1.RawValue  `asn1:"optional,tag:1,explicit,sequence"`
}

const (
	PKIStatusAccepted          = 0
	PKIStatusGrantedWithMods   = 1
	PKIStatusRejection         = 2
	PKIStatusWaiting           = 3
	PKIStatusRevocationWarning = 4
	PKIStatusRevocationNotif   = 5
	PKIStatusKeyUpdateWarning  = 6
)

const (
	FailInfoBadAlg              = 0
	FailInfoBadMessageCheck     = 1
	FailInfoBadRequest          = 2
	FailInfoBadTime             = 3
	FailInfoBadCertID           = 4
	FailInfoBadDataFormat       = 5
	FailInfoWrongAuthority      = 6
	FailInfoIncorrectData       = 7
	FailInfoMissingTimeStamp    = 8
	FailInfoBadPOP              = 9
	FailInfoCertRevoked         = 10
	FailInfoCertConfirmed       = 11
	FailInfoWrongIntegrity      = 12
	FailInfoBadRecipientNonce   = 13
	FailInfoTimeNotAvailable    = 14
	FailInfoUnacceptedPolicy    = 15
	FailInfoUnacceptedExtension = 16
	FailInfoAddInfoNotAvailable = 17
	FailInfoBadSenderNonce      = 18
	FailInfoBadCertTemplate     = 19
	FailInfoSignerNotTrusted    = 20
	FailInfoTransactionIDInUse  = 21
	FailInfoUnsupportedVersion  = 22
	FailInfoNotAuthorized       = 23
	FailInfoSystemUnavailable   = 24
	FailInfoSystemFailure       = 25
	FailInfoDuplicateCertReq    = 26
)

type ErrorMsgContent struct {
	Status      PKIStatusInfo
	ErrorCode   int           `asn1:"optional"`
	ErrorDetail asn1.RawValue `asn1:"optional"`
}

type PBMParameter struct {
	Salt           []byte
	OWF            pkiutil.AlgorithmIdentifier
	IterationCount int
	MAC            pkiutil.AlgorithmIdentifier
}

type POPPolicy struct {
	RequirePOP         bool
	AllowRAVerifiedPOP bool
	AllowSignaturePOP  bool
}
