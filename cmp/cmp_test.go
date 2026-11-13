package cmp_test

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/asn1"
	"encoding/hex"
	"testing"

	"github.com/otpki/otpki-pkg/cmp"
	"github.com/otpki/otpki-pkg/oids"
	"github.com/otpki/otpki-pkg/pkiutil"
)

func confirmation() *cmp.PKIMessage {
	name := asn1.RawValue{FullBytes: []byte{0xa4, 2, 0x30, 0}}
	return &cmp.PKIMessage{Header: cmp.PKIHeader{Pvno: 2, Sender: name, Recipient: name}, Body: cmp.PKIBody{Type: cmp.BodyTypePKIConf}}
}

func TestWireVectorAndOriginalProtectedPart(t *testing.T) {
	// Preserve the existing encoding, including the empty extraCerts wrapper.
	want, err := hex.DecodeString("3015300b020102a4023000a4023000b3020500a1023000")
	if err != nil {
		t.Fatal(err)
	}
	der, err := cmp.MarshalPKIMessage(confirmation())
	if err != nil || !bytes.Equal(der, want) {
		t.Fatalf("encoding = %x, %v; want %x", der, err, want)
	}
	msg, _, _, err := cmp.DecodePKIMessage(want)
	if err != nil {
		t.Fatal(err)
	}
	msg.Header.Pvno = 3
	wantSigned, err := hex.DecodeString("3011300b020102a4023000a4023000b3020500")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := cmp.ProtectedPartFromWire(msg)
	if err != nil || !bytes.Equal(wire, wantSigned) {
		t.Fatalf("original signed bytes changed: %x, %v", wire, err)
	}
	modified, err := cmp.MarshalProtectedPart(msg)
	if err != nil || bytes.Equal(modified, wire) {
		t.Fatalf("modified message did not re-encode: %x, %v", modified, err)
	}
}

func TestSignatureProtection(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg := confirmation()
	err = cmp.SignProtection(msg, key, pkiutil.AlgorithmIdentifier{Algorithm: oids.OidEd25519}, crypto.Hash(0), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := cmp.MarshalPKIMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, _, err := cmp.DecodePKIMessage(der)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := cmp.ProtectedPartFromWire(parsed)
	if err != nil || !ed25519.Verify(pub, signed, parsed.Protection.Bytes) {
		t.Fatalf("signature does not cover original wire bytes: %v", err)
	}
	signed[len(signed)-1] ^= 1
	if ed25519.Verify(pub, signed, parsed.Protection.Bytes) {
		t.Fatal("tampered protected part verified")
	}
}

func TestPBMIndependentVectors(t *testing.T) {
	// Fixed vectors computed independently with Python hashlib and hmac.
	for _, tc := range []struct {
		iterations int
		owf, mac   asn1.ObjectIdentifier
		want       string
	}{
		{1, oids.OidASN1SHA256, oids.OidHMACWithSHA256, "d38da645d38e459461ff9fc5585964546298ec87b125960430ce105c7b3c243c"},
		{1000, oids.OidASN1SHA256, oids.OidHMACWithSHA256, "e7fddd4097cdf20c4c01034862757cbd4ed8e46a65b617369cdde3da565151db"},
		{1000, oids.OidASN1SHA384, oids.OidHMACWithSHA384, "96f4520c09a3d43941859c02f238db6b7dc2d5b4ea26c9ff036ab495a7e2a6ef4f36f3df5e9f5d5f1a17fcf1f1014b42"},
	} {
		parameters := cmp.PBMParameter{Salt: []byte("0123456789abcdef"), IterationCount: tc.iterations, OWF: pkiutil.AlgorithmIdentifier{Algorithm: tc.owf}, MAC: pkiutil.AlgorithmIdentifier{Algorithm: tc.mac}}
		got, err := cmp.ComputePBM([]byte("password"), []byte("protected-part"), parameters, false)
		if err != nil || hex.EncodeToString(got) != tc.want {
			t.Fatalf("PBM(%v,%d) = %x, %v", tc.owf, tc.iterations, got, err)
		}
	}
}

func TestPBMRejectsInvalidWorkAndDisallowedAlgorithms(t *testing.T) {
	p := cmp.PBMParameter{IterationCount: 1, OWF: pkiutil.AlgorithmIdentifier{Algorithm: oids.OidASN1SHA256}, MAC: pkiutil.AlgorithmIdentifier{Algorithm: oids.OidHMACWithSHA256}}
	for _, iterations := range []int{-1, 0, cmp.MaxPBMIterations + 1} {
		p.IterationCount = iterations
		if _, err := cmp.ComputePBM(nil, nil, p, false); err == nil {
			t.Fatalf("accepted iteration count %d", iterations)
		}
	}
	p.IterationCount = 1
	p.OWF.Algorithm = oids.OidASN1SHA1
	p.MAC.Algorithm = oids.OidHMACWithSHA1_PKCS
	if _, err := cmp.ComputePBM(nil, nil, p, false); err == nil {
		t.Fatal("implicitly accepted SHA-1")
	}
	if _, err := cmp.ComputePBM(nil, nil, p, true); err != nil {
		t.Fatalf("explicit legacy policy failed: %v", err)
	}
	p.OWF.Algorithm = asn1.ObjectIdentifier{1, 2, 3}
	if _, err := cmp.ComputePBM(nil, nil, p, true); err == nil {
		t.Fatal("accepted unknown OWF")
	}
}
