package helpers

import "github.com/otpki/otpki-pkg/pkiutil"

// Compatibility aliases preserve existing callers while protocol packages use pkiutil.
type (
	AlgorithmIdentifier  = pkiutil.AlgorithmIdentifier
	SubjectPublicKeyInfo = pkiutil.SubjectPublicKeyInfo
)

const DerSequence = pkiutil.DerSequence

func EnsureDERSequence(b []byte) []byte { return pkiutil.EnsureDERSequence(b) }
func ASN1BitLength(b []byte) int        { return pkiutil.ASN1BitLength(b) }
func ReverseBitsInAByte(b byte) byte    { return pkiutil.ReverseBitsInAByte(b) }
