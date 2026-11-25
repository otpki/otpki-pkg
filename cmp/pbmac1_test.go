package cmp_test

import (
	"encoding/asn1"
	"encoding/hex"
	"math"
	"testing"

	"github.com/otpki/otpki-pkg/cmp"
	"github.com/otpki/otpki-pkg/oids"
	"github.com/otpki/otpki-pkg/pkiutil"
	"github.com/stretchr/testify/require"
)

func pbmac1Parameters(t *testing.T, kdf cmp.PBKDF2Params, mac asn1.ObjectIdentifier) cmp.PBMAC1Params {
	t.Helper()
	der, err := asn1.Marshal(kdf)
	require.NoError(t, err)
	return cmp.PBMAC1Params{
		KeyDerivationFunc: pkiutil.AlgorithmIdentifier{Algorithm: oids.OidPBKDF2, Parameters: asn1.RawValue{FullBytes: der}},
		MessageAuthScheme: pkiutil.AlgorithmIdentifier{Algorithm: mac},
	}
}

func TestPBMAC1KnownAnswers(t *testing.T) {
	t.Parallel()
	// Independently generated with Python hashlib.pbkdf2_hmac and hmac.new.
	for _, tt := range []struct {
		name      string
		prf, mac  asn1.ObjectIdentifier
		keyLength int
		allowSHA1 bool
		want      string
	}{
		{
			name:      "SHA256",
			prf:       oids.OidHMACWithSHA256,
			mac:       oids.OidHMACWithSHA256,
			keyLength: 32,
			want:      "425d466ca681c90044f3d03c432bba132a7db2eac5c2a8b5df4da45e809c13bc",
		},
		{
			name:      "SHA384",
			prf:       oids.OidHMACWithSHA384,
			mac:       oids.OidHMACWithSHA384,
			keyLength: 48,
			want:      "45110b7657545d693e5c8809176a874c18b55f945e23fe7c0f58e40edf715342ec917afbde55774f23f0f8d4d58ab572",
		},
		{
			name:      "SHA512",
			prf:       oids.OidHMACWithSHA512,
			mac:       oids.OidHMACWithSHA512,
			keyLength: 64,
			want:      "4b973b2f125d3d7635047f5bd224e712bfe5fb343d411d379b2bf73bc7260babfd42f4990d6f0a28d1772e3998b3f7566b45aa27c71377684a9b27e5bfeceef9",
		},
		{
			name:      "different PRF and MAC",
			prf:       oids.OidHMACWithSHA256,
			mac:       oids.OidHMACWithSHA512,
			keyLength: 64,
			want:      "5efe1f5a079179a2a43088ac2408f2ea1f1c423068e007952ae7fe42f2193fc233f7c33083d65ce2796484b2fbc4943626e3d8cadbdb8c42cc0e12175674d2eb",
		},
		{
			name:      "default PRF",
			mac:       oids.OidHMACWithSHA256,
			keyLength: 32,
			allowSHA1: true,
			want:      "30c6ea962cd16eea1ba47d05b3024273b4dc849c12fe9adb4ad78f8af665bd1d",
		},
		{
			name: "omitted key length",
			prf:  oids.OidHMACWithSHA256,
			mac:  oids.OidHMACWithSHA256,
			want: "425d466ca681c90044f3d03c432bba132a7db2eac5c2a8b5df4da45e809c13bc",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parameters := pbmac1Parameters(t, cmp.PBKDF2Params{
				Salt:           asn1.RawValue{Tag: asn1.TagOctetString, Bytes: []byte("salt1234")},
				IterationCount: 1945,
				KeyLength:      tt.keyLength,
				PRF:            pkiutil.AlgorithmIdentifier{Algorithm: tt.prf},
			}, tt.mac)
			got, err := cmp.ComputePBMAC1([]byte("password"), []byte("protected-part"), parameters, tt.allowSHA1)
			require.NoError(t, err)
			require.Equal(t, tt.want, hex.EncodeToString(got))
		})
	}
}

func TestPBMAC1RejectsInvalidParameters(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"zero iterations", "negative iterations", "excessive iterations",
		"negative key length", "huge key length", "short salt", "long salt",
		"other salt source", "unsupported PRF", "default SHA1", "unsupported MAC",
		"unsupported KDF", "malformed KDF", "trailing KDF data",
	} {
		t.Run(name, func(t *testing.T) {
			kdf := cmp.PBKDF2Params{
				Salt:           asn1.RawValue{Tag: asn1.TagOctetString, Bytes: []byte("salt1234")},
				IterationCount: 1945,
				KeyLength:      32,
				PRF:            pkiutil.AlgorithmIdentifier{Algorithm: oids.OidHMACWithSHA256},
			}
			switch name {
			case "zero iterations":
				kdf.IterationCount = 0
			case "negative iterations":
				kdf.IterationCount = -1
			case "excessive iterations":
				kdf.IterationCount = cmp.MaxPBMAC1Iterations + 1
			case "negative key length":
				kdf.KeyLength = -1
			case "huge key length":
				kdf.KeyLength = math.MaxInt
			case "short salt":
				kdf.Salt.Bytes = []byte("short")
			case "long salt":
				kdf.Salt.Bytes = make([]byte, 1025)
			case "other salt source":
				kdf.Salt = asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true}
			case "unsupported PRF":
				kdf.PRF.Algorithm = asn1.ObjectIdentifier{1, 2, 3}
			case "default SHA1":
				kdf.PRF = pkiutil.AlgorithmIdentifier{}
			}
			parameters := pbmac1Parameters(t, kdf, oids.OidHMACWithSHA256)
			switch name {
			case "unsupported MAC":
				parameters.MessageAuthScheme.Algorithm = asn1.ObjectIdentifier{1, 2, 3}
			case "unsupported KDF":
				parameters.KeyDerivationFunc.Algorithm = asn1.ObjectIdentifier{1, 2, 3}
			case "malformed KDF":
				parameters.KeyDerivationFunc.Parameters.FullBytes = []byte{0x30, 0xff}
			case "trailing KDF data":
				parameters.KeyDerivationFunc.Parameters.FullBytes = append(parameters.KeyDerivationFunc.Parameters.FullBytes, 0)
			}
			_, err := cmp.ComputePBMAC1([]byte("password"), []byte("protected-part"), parameters, false)
			require.Error(t, err)
		})
	}
}
