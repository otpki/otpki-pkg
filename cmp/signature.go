package cmp

import (
	"crypto"
	"crypto/rand"
	"encoding/asn1"
	"fmt"
	"io"

	"github.com/otpki/otpki-pkg/pkiutil"
)

// SignProtection signs the ProtectedPart with the caller-selected algorithm.
// Certificate selection, trust, extraCerts, and authorization remain with callers.
func SignProtection(msg *PKIMessage, key crypto.Signer, algorithm pkiutil.AlgorithmIdentifier, hash crypto.Hash, random io.Reader) error {
	if msg == nil || key == nil {
		return fmt.Errorf("signature protection requires a message and signer")
	}
	if random == nil {
		random = rand.Reader
	}
	if err := msg.Header.SetProtectionAlg(algorithm); err != nil {
		return err
	}
	message, err := MarshalProtectedPart(msg)
	if err != nil {
		return err
	}
	signature, err := crypto.SignMessage(key, random, message, hash)
	if err != nil {
		return err
	}
	msg.Protection = &asn1.BitString{Bytes: signature, BitLength: len(signature) * 8}
	return nil
}
