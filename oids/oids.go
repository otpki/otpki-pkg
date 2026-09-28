package oids

import (
	"encoding/asn1"
)

var (
	// MS specified OIDs
	// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-wcce/f4eb50d5-62f6-470b-8846-9489af9dea62
	OidOSVersion               = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 13, 2, 3}
	OidEnrollmentCspProvider   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 13, 2, 2}
	OidRenewalCertificate      = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 13, 1}
	OidRequestClientInfo       = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 21, 20}
	OidCMCAddAttributes        = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 10, 10, 1}
	OidMSUserPrincipalName     = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 20, 2, 3}
	OidIssuedCertHash          = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 21, 17}
	OidCertificateTemplateInfo = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 21, 7}
	OidCertificateTemplateName = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 20, 2}
	OidCTPKIResponse           = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 12, 3}
	OidCMCStatusInfo           = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 1}

	// CMC controls — RFC 10002 (Certificate Management over CMS, obsoletes
	// RFCs 5272 and 6402). id-cmc OBJECT IDENTIFIER ::= { id-pkix 7 }
	OidCMCIdentification        = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 2}
	OidCMCIdentityProof         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 3}
	OidCMCDataReturn            = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 4}
	OidCMCTransactionID         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 5}
	OidCMCSenderNonce           = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 6}
	OidCMCRecipientNonce        = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 7}
	OidCMCAddExtensions         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 8}
	OidCMCEncryptedPOP          = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 9}
	OidCMCDecryptedPOP          = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 10}
	OidCMCLRAPOPWitness         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 11}
	OidCMCGetCert               = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 15}
	OidCMCGetCRL                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 16}
	OidCMCRevokeRequest         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 17}
	OidCMCRegInfo               = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 18}
	OidCMCResponseInfo          = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 19}
	OidCMCQueryPending          = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 21}
	OidCMCPopLinkRandom         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 22}
	OidCMCPopLinkWitness        = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 23}
	OidCMCConfirmCertAcceptance = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 24}
	OidCMCStatusInfoV2          = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 25}
	OidCMCTrustedAnchors        = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 26}
	OidCMCAuthData              = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 27}
	OidCMCBatchRequests         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 28}
	OidCMCBatchResponses        = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 29}
	OidCMCPublishCert           = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 30}
	OidCMCModCertTemplate       = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 31}
	OidCMCControlProcessed      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 32}
	OidCMCPopLinkWitnessV2      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 33}
	OidCMCIdentityProofV2       = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 34}
	OidCMCRAIdentityWitness     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 35}
	OidCMCChangeSubjectName     = OidAttributeChangeSubjectName // id-cmc 36
	OidCMCResponseBody          = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 37}

	// CMC content types — id-cct OBJECT IDENTIFIER ::= { id-pkix 12 }
	OidCTPKIData = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 12, 2}

	// id-aa-cmcUnsignedData - { smime(16) id-aa(2) 34 }, carries an unsigned
	// PKIResponse inside an AuthenticatedData envelope (RFC 10002).
	OidAACMCUnsignedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 34}

	// id-alg-noSignature — { id-pkix id-alg(6) 2 }: on a certification request
	// for a key pair that cannot sign (POP via encrypted/decrypted POP).
	OidAlgNoSignature = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 6, 2}

	// id-ad-cmc — { id-ad 12 }, CMC access descriptor.
	OidADCMC = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 12}

	OidSubjectAltName             = asn1.ObjectIdentifier{2, 5, 29, 17}
	OidIssuerAltName              = asn1.ObjectIdentifier{2, 5, 29, 18}
	OidKerberosPrincipalName      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 2, 2}
	OidHardwareModuleName         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 8, 4}
	OidDomainComponent            = asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 25}
	OidDeltaCRLIndicator          = asn1.ObjectIdentifier{2, 5, 29, 27}
	OidKeyUsage                   = asn1.ObjectIdentifier{2, 5, 29, 15}
	OidExtKeyUsage                = asn1.ObjectIdentifier{2, 5, 29, 37} // id-ce-extKeyUsage
	OidNameConstraints            = asn1.ObjectIdentifier{2, 5, 29, 30}
	OidCrlDistributionPoints      = asn1.ObjectIdentifier{2, 5, 29, 31}
	OidFreshestCrl                = asn1.ObjectIdentifier{2, 5, 29, 46} // id-ce-freshestCRL delta crl cdp
	OidCARepoLocation             = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 5}
	OidAttributeChangeSubjectName = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 36}

	OidOCSPAD                           = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1}    // id-ad-ocsp
	OidOCSPBasic                        = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 1} // id-pkix-ocsp-basic
	OidOCSPNonce                        = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 2} // id-pkix-ocsp-nonce
	OidOCSPCRLReferences                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 3} // id-pkix-ocsp-crl
	OidOCSPResponseTypes                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 4} // id-pkix-ocsp-response
	OidOCSPNoCheck                      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 5}
	OidOCSPArchiveCutoff                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 6} // id-pkix-ocsp-archive-cutoff
	OidOCSPServiceLocator               = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 7} // id-pkix-ocsp-service-locator
	OidOCSPPreferredSignatureAlgorithms = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 8} // id-pkix-ocsp-pref-sig-algs
	OidOCSPExtendedRevoke               = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 9} // id-pkix-ocsp-extended-revoke

	OidASN1SHA1                 = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	OidASN1SHA224               = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 4}
	OidASN1SHA256               = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	OidASN1SHA384               = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	OidASN1SHA512               = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	OidASN1SHA3_224             = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 7}
	OidASN1SHA3_256             = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 8}
	OidASN1SHA3_384             = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 9}
	OidASN1SHA3_512             = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 10}
	OidMD5WithRSA               = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 4}
	OidSignatureSHA1WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	OidMGF1                     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 8}
	OidRSASSAPSS                = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	OidSignatureSHA256WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	OidSignatureSHA384WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	OidSignatureSHA512WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	OidSignatureECDSAWithSHA1   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
	OidSignatureECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	OidSignatureECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	OidSignatureECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}

	// EC Curves
	OidECP192 = asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 1}
	OidECP224 = asn1.ObjectIdentifier{1, 3, 132, 0, 33}
	OidECP256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7}
	OidECP384 = asn1.ObjectIdentifier{1, 3, 132, 0, 34}
	OidECP521 = asn1.ObjectIdentifier{1, 3, 132, 0, 35}

	OidExtKeyUsageAny                            = asn1.ObjectIdentifier{2, 5, 29, 37, 0}
	OidExtKeyUsageServerAuth                     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 1}
	OidExtKeyUsageClientAuth                     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 2}
	OidExtKeyUsageCodeSigning                    = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 3}
	OidExtKeyUsageEmailProtection                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 4}
	OidExtKeyUsageIPSECEndSystem                 = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 5}
	OidExtKeyUsageIPSECTunnel                    = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 6}
	OidExtKeyUsageIPSECUser                      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 7}
	OidExtKeyUsageTimeStamping                   = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 8}
	OidExtKeyUsageOCSPSigning                    = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 9}
	OidExtKeyUsageMicrosoftServerGatedCrypto     = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 10, 3, 3}
	OidExtKeyUsageNetscapeServerGatedCrypto      = asn1.ObjectIdentifier{2, 16, 840, 1, 113730, 4, 1}
	OidExtKeyUsageMicrosoftCommercialCodeSigning = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 2, 1, 22}
	OidExtKeyUsageMicrosoftKernelCodeSigning     = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 61, 1, 1}

	// CMC key purposes for authority certificates (RFC 10002 A.1)
	OidExtKeyUsageCMCCA      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 27}
	OidExtKeyUsageCMCRA      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 28}
	OidExtKeyUsageCMCArchive = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 29}

	OidCountry            = asn1.ObjectIdentifier{2, 5, 4, 6}
	OidOrganization       = asn1.ObjectIdentifier{2, 5, 4, 10}
	OidOrganizationalUnit = asn1.ObjectIdentifier{2, 5, 4, 11}
	OidCommonName         = asn1.ObjectIdentifier{2, 5, 4, 3}
	OidSerialNumber       = asn1.ObjectIdentifier{2, 5, 4, 5}
	OidLocality           = asn1.ObjectIdentifier{2, 5, 4, 7}
	OidProvince           = asn1.ObjectIdentifier{2, 5, 4, 8}
	OidStreetAddress      = asn1.ObjectIdentifier{2, 5, 4, 9}
	OidPostalCode         = asn1.ObjectIdentifier{2, 5, 4, 17}
	OidEmail              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}

	OidReasonCode          = asn1.ObjectIdentifier{2, 5, 29, 21}
	OidHoldInstructionCode = asn1.ObjectIdentifier{2, 5, 29, 23}
	OidInvalidityDate      = asn1.ObjectIdentifier{2, 5, 29, 24}
	OidCertificateIssuer   = asn1.ObjectIdentifier{2, 5, 29, 29}

	OidPasswordBasedMAC = asn1.ObjectIdentifier{1, 2, 840, 113533, 7, 66, 13}
	OidPBMAC1           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 14}
	OidPBKDF2           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}

	OidHMACWithSHA1_PKCS = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	OidHMACWithSHA1_IKE  = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 8, 1, 2}
	OidHMACWithSHA224    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 8}
	OidHMACWithSHA256    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	OidHMACWithSHA384    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 10}
	OidHMACWithSHA512    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 11}

	// ML-DSA (FIPS 204)
	OidMLDSA44 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 17}
	OidMLDSA65 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 18}
	OidMLDSA87 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 19}

	// ML-KEM (FIPS 203)
	OidMLKEM512  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 4, 1}
	OidMLKEM768  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 4, 2}
	OidMLKEM1024 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 4, 3}

	// SLH-DSA (FIPS 205)
	OidSLHDSASHA2_128s  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 20}
	OidSLHDSASHA2_128f  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 21}
	OidSLHDSASHA2_192s  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 22}
	OidSLHDSASHA2_192f  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 23}
	OidSLHDSASHA2_256s  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 24}
	OidSLHDSASHA2_256f  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 25}
	OidSLHDSASHAKE_128s = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 26}
	OidSLHDSASHAKE_128f = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 27}
	OidSLHDSASHAKE_192s = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 28}
	OidSLHDSASHAKE_192f = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 29}
	OidSLHDSASHAKE_256s = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 30}
	OidSLHDSASHAKE_256f = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 31}

	// HSS/LMS (RFC 8708)
	OidHSSLMS = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 3, 17}

	// XMSS / XMSS-MT (draft-vangeest-x509-hash-sigs)
	OidXMSS   = asn1.ObjectIdentifier{0, 4, 0, 127, 0, 15, 1, 1, 13, 0}
	OidXMSSMT = asn1.ObjectIdentifier{0, 4, 0, 127, 0, 15, 1, 1, 14, 0}

	// EdDSA (RFC 8410)
	OidEd25519 = asn1.ObjectIdentifier{1, 3, 101, 112}
	OidEd448   = asn1.ObjectIdentifier{1, 3, 101, 113}

	// ECDSA with SHA3
	OidECDSAWithSHA3_256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 10}
	OidECDSAWithSHA3_384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 11}
	OidECDSAWithSHA3_512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 12}

	// RSA PKCS#1 v1.5 with SHA3
	OidRSAPKCS1v15WithSHA3_256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 14}
	OidRSAPKCS1v15WithSHA3_384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 15}
	OidRSAPKCS1v15WithSHA3_512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 16}

	// SHAKE signature OIDs (RFC 8692)
	OidRSASSAPSSWithSHAKE128 = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 6, 30}
	OidRSASSAPSSWithSHAKE256 = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 6, 31}
	OidECDSAWithSHAKE128     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 6, 32}
	OidECDSAWithSHAKE256     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 6, 33}

	// ACME OID
	OidACMEIdentifier = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}

	// CMS content types (RFC 5652 / RFC 5083 / RFC 5084)
	OidContentTypeData              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	OidContentTypeSignedData        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	OidContentTypeEnvelopedData     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 3}
	OidContentTypeEncryptedData     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 6}
	OidContentTypeAuthenticatedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 2}
	OidContentTypeAuthEnvelopedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 23}

	// CMS signed attributes (RFC 5652 / PKCS #9)
	OidAttributeContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	OidAttributeMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}

	// PKCS #9 extensionRequest attribute carried inside a CertificationRequest
	OidExtensionRequest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 14}

	// AES content encryption (RFC 3565)
	OidAES128CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	OidAES192CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	OidAES256CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	OidAES128GCM = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 6}
	OidAES192GCM = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 26}
	OidAES256GCM = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 46}

	// AES key wrap (RFC 3394), used inside KeyAgreeRecipientInfo
	OidAES128Wrap = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 5}
	OidAES192Wrap = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 25}
	OidAES256Wrap = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 45}

	// Key transport / key agreement (PKCS #1, SEC 1 / ANSI x9.63 schemes
	// used by CMS EnvelopedData)
	OidRSAEncryption              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	OidRSAESOAEP                  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 7}
	OidDHSinglePassStdDHSHA1KDF   = asn1.ObjectIdentifier{1, 3, 133, 16, 840, 63, 0, 2}  // dhSinglePass-stdDH-sha1kdf-scheme
	OidDHSinglePassStdDHSHA256KDF = asn1.ObjectIdentifier{1, 3, 132, 1, 11, 1}           // dhSinglePass-stdDH-sha256kdf-scheme
	OidDHSinglePassStdDHSHA384KDF = asn1.ObjectIdentifier{1, 3, 132, 1, 11, 2}           // dhSinglePass-stdDH-sha384kdf-scheme
	OidDHSinglePassStdDHSHA512KDF = asn1.ObjectIdentifier{1, 3, 132, 1, 11, 3}           // dhSinglePass-stdDH-sha512kdf-scheme
	OidMQVSinglePassSHA1KDF       = asn1.ObjectIdentifier{1, 3, 133, 16, 840, 63, 0, 16} // mqvSinglePass-sha1kdf-scheme
)
