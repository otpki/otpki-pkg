package crmf_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/asn1"
	"testing"

	"github.com/otpki/otpki-pkg/crmf"
	"github.com/otpki/otpki-pkg/oids"
	"github.com/otpki/otpki-pkg/pkiutil"
)

func TestSignatureInputUsesOriginalRequestAndWrapsImplicitInput(t *testing.T) {
	original := []byte{0x30, 0x03, 0x02, 0x01, 0x07}
	got, err := crmf.SignatureInput(original, asn1.RawValue{})
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("lost original request DER: %x, %v", got, err)
	}
	// The implicit contents can themselves begin with a SEQUENCE. They must
	// still receive the outer SEQUENCE that is covered by the signature.
	input := asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: []byte{0x30, 0, 0x30, 0}}
	got, err = crmf.SignatureInput(original, input)
	if err != nil || !bytes.Equal(got, []byte{0x30, 4, 0x30, 0, 0x30, 0}) {
		t.Fatalf("wrong implicit input: %x, %v", got, err)
	}
	if _, err := crmf.SignatureInput(nil, asn1.RawValue{}); err == nil {
		t.Fatal("accepted missing signature input")
	}
}

func TestSignaturePOPAndRawRequestPreservation(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	request := crmf.CertRequest{CertReqID: 7, CertTemplate: crmf.CertTemplate{PublicKey: pkiutil.SubjectPublicKeyInfo{Algorithm: pkiutil.AlgorithmIdentifier{Algorithm: oids.OidEd25519}, SubjectPublicKey: asn1.BitString{Bytes: pub, BitLength: len(pub) * 8}}}}
	der, err := asn1.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	pop, err := crmf.BuildSignaturePOP(der, key, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := crmf.ParsePOPOSigningKey(pop.Bytes)
	if err != nil || !ed25519.Verify(pub, der, parsed.Signature.Bytes) {
		t.Fatalf("POP signature verification failed: %v", err)
	}
	messageDER, err := asn1.Marshal(crmf.CertReqMessages{{CertReq: request, Popo: pop}})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := crmf.UnmarshalCertReqMessagesWithRaw(messageDER)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !bytes.Equal(messages[0].RawCertReqDER, der) {
		t.Fatal("decoder did not preserve the signed CertRequest DER")
	}
	messages[0].CertReq.CertReqID++
	signed, err := crmf.SignatureInput(messages[0].RawCertReqDER, asn1.RawValue{})
	if err != nil || !ed25519.Verify(pub, signed, parsed.Signature.Bytes) {
		t.Fatalf("verification re-encoded the mutated request: %v", err)
	}
	gotPub, err := crmf.PublicKey(request)
	if err != nil {
		t.Fatal(err)
	}
	sa, err := pkiutil.SigAlgFromAlgID(parsed.AlgID.Algorithm, &parsed.AlgID.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if err := pkiutil.VerifySignature(gotPub, sa, signed, parsed.Signature.Bytes); err != nil {
		t.Fatal(err)
	}
	if _, err := crmf.PublicKey(crmf.CertRequest{}); err == nil {
		t.Fatal("accepted absent public key")
	}
}
