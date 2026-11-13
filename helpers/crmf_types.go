package helpers

import "github.com/otpki/otpki-pkg/crmf"

type (
	AttributeTypeAndValue = crmf.AttributeTypeAndValue
	CertTemplate          = crmf.CertTemplate
	CertRequest           = crmf.CertRequest
	CertReqMsg            = crmf.CertReqMsg
	CertReqMessages       = crmf.CertReqMessages
)

func UnmarshalCertReqMessagesWithRaw(der []byte) (CertReqMessages, error) {
	return crmf.UnmarshalCertReqMessagesWithRaw(der)
}
func MarshalCertReqMsgDER(m CertReqMsg) ([]byte, error)     { return crmf.MarshalCertReqMsgDER(m) }
func UnmarshalCertReqMsgDER(der []byte) (CertReqMsg, error) { return crmf.UnmarshalCertReqMsgDER(der) }
