package pkiutil_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/otpki/otpki-pkg/pkiutil"
)

func TestSignerAlgorithmsAndSignatureVerification(t *testing.T) {
	for name, generate := range map[string]func() (crypto.Signer, error){
		"RSA":     func() (crypto.Signer, error) { return rsa.GenerateKey(rand.Reader, 2048) },
		"ECDSA":   func() (crypto.Signer, error) { return ecdsa.GenerateKey(elliptic.P256(), rand.Reader) },
		"Ed25519": func() (crypto.Signer, error) { _, key, err := ed25519.GenerateKey(rand.Reader); return key, err },
	} {
		t.Run(name, func(t *testing.T) {
			key, err := generate()
			if err != nil {
				t.Fatal(err)
			}
			alg, hash, err := pkiutil.SignerAlgorithmFromPublicKey(key.Public())
			if err != nil {
				t.Fatal(err)
			}
			message := []byte("shared signature verification")
			sig, err := crypto.SignMessage(key, rand.Reader, message, hash)
			if err != nil {
				t.Fatal(err)
			}
			sa, err := pkiutil.SigAlgFromAlgID(alg.Algorithm, &alg.Parameters)
			if err != nil {
				t.Fatal(err)
			}
			if err := pkiutil.VerifySignature(key.Public(), sa, message, sig); err != nil {
				t.Fatal(err)
			}
			message[0] ^= 1
			if err := pkiutil.VerifySignature(key.Public(), sa, message, sig); err == nil {
				t.Fatal("accepted tampered signed message")
			}
		})
	}
}
