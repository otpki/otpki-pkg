package oids

import (
	"encoding/asn1"
)

var (
	// MS specified OIDs (https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-wcce/f4eb50d5-62f6-470b-8846-9489af9dea62)
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

	OidSubjectAltName             = asn1.ObjectIdentifier{2, 5, 29, 17}
	OidIssuerAltName              = asn1.ObjectIdentifier{2, 5, 29, 18}
	OidKerberosPrincipalName      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 2, 2}
	OidDomainComponent            = asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 25}
	OidDeltaCRLIndicator          = asn1.ObjectIdentifier{2, 5, 29, 27}
	OidKeyUsage                   = asn1.ObjectIdentifier{2, 5, 29, 15}
	OidExtKeyUsage                = asn1.ObjectIdentifier{2, 5, 29, 37} // id-ce-extKeyUsage
	OidNameConstraints            = asn1.ObjectIdentifier{2, 5, 29, 30}
	OidCrlDistributionPoints      = asn1.ObjectIdentifier{2, 5, 29, 31}
	OidCARepoLocation             = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 5}
	OidAttributeChangeSubjectName = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 7, 36}

	OidADOCSP                       = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1}    // id-ad-ocsp
	OidPKIXOCSPBasic                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 1} // id-pkix-ocsp-basic
	OidNonce                        = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 2} // id-pkix-ocsp-nonce
	OidCRLReferences                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 3} // id-pkix-ocsp-crl
	OidResponseTypes                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 4} // id-pkix-ocsp-response (AcceptableResponseTypes)
	OidArchiveCutoff                = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 6} // id-pkix-ocsp-archive-cutoff
	OidServiceLocator               = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 7} // id-pkix-ocsp-service-locator
	OidPreferredSignatureAlgorithms = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 8} // id-pkix-ocsp-pref-sig-algs
	OidExtendedRevoke               = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 48, 1, 9} // id-pkix-ocsp-extended-revoke
	OidASN1SHA1                     = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	OidASN1SHA256                   = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	OidASN1SHA384                   = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	OidASN1SHA512                   = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	OidMD5WithRSA                   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 4}
	OidSignatureSHA1WithRSA         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	OidMGF1                         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 8}
	OidRSASSAPSS                    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	OidSignatureSHA256WithRSA       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	OidSignatureSHA384WithRSA       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	OidSignatureSHA512WithRSA       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	OidSignatureECDSAWithSHA1       = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
	OidSignatureECDSAWithSHA256     = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	OidSignatureECDSAWithSHA384     = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	OidSignatureECDSAWithSHA512     = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}

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

	OidReasonCode = asn1.ObjectIdentifier{2, 5, 29, 21}

	OidPasswordBasedMAC = asn1.ObjectIdentifier{1, 2, 840, 113533, 7, 66, 13}
	OidPBMAC1           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 14}
	OidPBKDF2           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}

	OidHMACWithSHA1_PKCS = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	OidHMACWithSHA1_IKE  = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 8, 1, 2}
	OidHMACWithSHA256    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	OidHMACWithSHA384    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 10}
	OidHMACWithSHA512    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 11}

	// ML-DSA (FIPS 204)
	OidMLDSA44 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 17}
	OidMLDSA65 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 18}
	OidMLDSA87 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 19}

	// Hash-SLH-DSA (FIPS 205)
	OidHashSLHDSASHA2_128s  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 35}
	OidHashSLHDSASHA2_128f  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 36}
	OidHashSLHDSASHA2_192s  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 37}
	OidHashSLHDSASHA2_192f  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 38}
	OidHashSLHDSASHA2_256s  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 39}
	OidHashSLHDSASHA2_256f  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 40}
	OidHashSLHDSASHAKE_128s = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 41}
	OidHashSLHDSASHAKE_128f = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 42}
	OidHashSLHDSASHAKE_192s = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 43}
	OidHashSLHDSASHAKE_192f = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 44}
	OidHashSLHDSASHAKE_256s = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 45}
	OidHashSLHDSASHAKE_256f = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 46}

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
)
