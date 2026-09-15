package uris

import (
	"net/url"
	"strings"
	"testing"
)

func TestEncodeComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"unreserved", "plain-Value_9.~x", "plain-Value_9.~x"},
		{"space", "My CA", "My%20CA"},
		{"structure", "a/b?c=d&e", "a%2Fb%3Fc%3Dd%26e"},
		{"distinguished name", "CN=Test CA,O=Org", "CN%3DTest%20CA%2CO%3DOrg"},
		{"percent", "100%", "100%25"},
		{"plus", "a+b", "a%2Bb"},
		{"unicode", "café", "caf%C3%A9"},
		{"fragment", "a#b", "a%23b"},
		{"all reserved", ":/?#[]@!$&'()*+,;=", "%3A%2F%3F%23%5B%5D%40%21%24%26%27%28%29%2A%2B%2C%3B%3D"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := EncodeComponent(tt.in); got != tt.want {
				t.Fatalf("EncodeComponent(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"path space", "http://localhost/My CA/crl.crl", "http://localhost/My%20CA/crl.crl"},
		{"query space", "http://localhost/crl?partId=My Part", "http://localhost/crl?partId=My%20Part"},
		{"existing escape", "http://localhost/My%20CA/crl.crl", "http://localhost/My%20CA/crl.crl"},
		{"query plus", "http://localhost/a?x=1+2", "http://localhost/a?x=1+2"},
		{"stray query percent", "http://localhost/a?x=%zz", "http://localhost/a?x=%25zz"},
		{"ldap DN", "ldap:///CN=My CA,O=X", "ldap:///CN=My%20CA,O=X"},
		{"fragment", "http://localhost/a#frag ment", "http://localhost/a#frag%20ment"},
		{"opaque", "mailto:a b@c.com", "mailto:a%20b@c.com"},
		{"query structure", "http://localhost/v1/validation/cdp.crl?certHash=abc123&partId=1&freshest=true", "http://localhost/v1/validation/cdp.crl?certHash=abc123&partId=1&freshest=true"},
		{"unicode query", "https://example.test/?q=café", "https://example.test/?q=caf%C3%A9"},
		{"braces query", "https://example.test/?q={value}", "https://example.test/?q=%7Bvalue%7D"},
		{"encoded slash", "https://example.test/a%2Fb", "https://example.test/a%2Fb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Normalize(tt.in)
			if err != nil {
				t.Fatalf("Normalize(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if _, err := url.Parse(got); err != nil {
				t.Fatalf("Normalize(%q) produced invalid URI %q: %v", tt.in, got, err)
			}
		})
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"http://localhost/My CA/crl.crl?x=a b&y=%20",
		"ldap:///CN=My CA,O=X",
		"mailto:a b@c.com",
		"https://example.test/a%2Fb?q={x}#hello world",
	}

	for _, in := range inputs {
		first, err := Normalize(in)
		if err != nil {
			t.Fatalf("Normalize(%q): %v", in, err)
		}
		second, err := Normalize(first)
		if err != nil {
			t.Fatalf("Normalize(%q) second pass: %v", in, err)
		}
		if first != second {
			t.Fatalf("Normalize is not idempotent: first %q, second %q", first, second)
		}
	}
}

func TestNormalizeErrors(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"http://my host/x",
		"http://localhost/a%zz",
		"http://[::1",
	} {
		if got, err := Normalize(in); err == nil {
			t.Fatalf("Normalize(%q) = %q, want error", in, got)
		}
	}
}

func TestEscapeInvalidPreservesValidPercentTriplets(t *testing.T) {
	t.Parallel()

	got := escapeInvalid("x=%2f%2F%zz%")
	want := "x=%2f%2F%25zz%25"
	if got != want {
		t.Fatalf("escapeInvalid = %q, want %q", got, want)
	}
}

func FuzzEncodeComponentNeverAddsStructure(f *testing.F) {
	for _, seed := range []string{"hello", "a/b?c=d&e", "café", "%00", "#fragment"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		encoded := EncodeComponent(s)
		if strings.ContainsAny(encoded, "/?#&=+,:;@!$'()*[]") {
			t.Fatalf("EncodeComponent(%q) left structural byte in %q", s, encoded)
		}
		if strings.Contains(encoded, "%") {
			for i := 0; i < len(encoded); i++ {
				if encoded[i] != '%' {
					continue
				}
				if i+2 >= len(encoded) || !isHex(encoded[i+1]) || !isHex(encoded[i+2]) {
					t.Fatalf("invalid percent escape in %q", encoded)
				}
				i += 2
			}
		}
	})
}
