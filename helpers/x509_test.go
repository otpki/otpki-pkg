package helpers_test

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"testing"

	"github.com/otpki/otpki-pkg/helpers"
)

func TestAddOrUpdateExt(t *testing.T) {
	t.Parallel()
	exts := []pkix.Extension{}
	exts = append(exts, pkix.Extension{Id: asn1.ObjectIdentifier{1, 1, 1}, Value: []byte("hello")})
	exts = helpers.AddOrUpdateExtension(exts, asn1.ObjectIdentifier{2, 2, 2}, []byte("newvalue"))
	if len(exts) != 2 {
		t.Fatalf("expected to add to ext length")
	}
	exts = helpers.AddOrUpdateExtension(exts, asn1.ObjectIdentifier{1, 1, 1}, []byte("hello2"))
	if len(exts) != 2 {
		t.Fatalf("expected to update existing ext")
	}
}
