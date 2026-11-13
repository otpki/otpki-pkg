package pkiutil_test

import (
	"bytes"
	"encoding/asn1"
	"fmt"
	"testing"

	"github.com/otpki/otpki-pkg/pkiutil"
)

func TestEnsureDERSequenceLengthBoundaries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		length int
		header []byte
	}{
		{1, []byte{0x30, 0x01}},
		{127, []byte{0x30, 0x7f}},
		{128, []byte{0x30, 0x81, 0x80}},
		{255, []byte{0x30, 0x81, 0xff}},
		{256, []byte{0x30, 0x82, 0x01, 0x00}},
		{65535, []byte{0x30, 0x82, 0xff, 0xff}},
		{65536, []byte{0x30, 0x83, 0x01, 0x00, 0x00}},
	} {
		t.Run(fmt.Sprint(tc.length), func(t *testing.T) {
			t.Parallel()
			content := bytes.Repeat([]byte{0x01}, tc.length)
			der := pkiutil.EnsureDERSequence(content)
			if !bytes.HasPrefix(der, tc.header) {
				t.Fatalf("DER header = %x, want %x", der[:len(tc.header)], tc.header)
			}
			var sequence asn1.RawValue
			rest, err := asn1.Unmarshal(der, &sequence)
			if err != nil {
				t.Fatal(err)
			}
			if len(rest) != 0 || sequence.Tag != asn1.TagSequence || !sequence.IsCompound || !bytes.Equal(sequence.Bytes, content) {
				t.Fatal("DER sequence did not preserve its contents")
			}
			if !bytes.Equal(content, bytes.Repeat([]byte{0x01}, tc.length)) {
				t.Fatal("input was modified")
			}
		})
	}
}

func TestEnsureDERSequencePreservesEmptyAndWrappedInput(t *testing.T) {
	t.Parallel()
	for _, input := range [][]byte{nil, {}, {0x30, 0x00}} {
		if got := pkiutil.EnsureDERSequence(input); !bytes.Equal(got, input) {
			t.Fatalf("EnsureDERSequence(%x) = %x", input, got)
		}
	}
}
