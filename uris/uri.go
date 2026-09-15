// Package uris provides helpers for constructing and parsing URIs, including
// PKCS #11 URIs as defined by RFC 7512.
package uris

import (
	"net/url"
	"strings"
)

const upperHex = "0123456789ABCDEF"

func isUnreserved(c byte) bool {
	return c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

// EncodeComponent percent-encodes s as a single URI value. Only RFC 3986
// unreserved bytes are left literal, so the result cannot introduce URI
// structure regardless of where it is inserted.
func EncodeComponent(s string) string {
	return percentEncode(s, isUnreserved)
}

// Normalize parses raw and returns a safely escaped URI. Existing valid
// percent-encoded octets and URI structure are preserved; bytes that cannot
// appear literally are percent-encoded.
func Normalize(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	if u.Opaque != "" {
		u.Opaque = escapeInvalid(u.Opaque)
	}
	u.RawQuery = escapeInvalid(u.RawQuery)

	return u.String(), nil
}

// escapeInvalid escapes bytes that may not appear literally in a URI query
// or opaque component. Valid percent triplets are preserved; a stray '%' is
// escaped as "%25".
func escapeInvalid(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteString(s[i : i+3])
			i += 2
			continue
		}
		if isQueryChar(c) {
			b.WriteByte(c)
			continue
		}
		writePercentByte(&b, c)
	}

	return b.String()
}

// isQueryChar reports whether c may appear literally in an RFC 3986 query
// component (pchar / "/" / "?"). '#' is intentionally excluded because it
// starts a fragment.
func isQueryChar(c byte) bool {
	if isUnreserved(c) {
		return true
	}
	switch c {
	case '!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=', ':', '@', '/', '?':
		return true
	default:
		return false
	}
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func percentEncode(s string, safe func(byte) bool) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if safe(s[i]) {
			b.WriteByte(s[i])
			continue
		}
		writePercentByte(&b, s[i])
	}

	return b.String()
}

func writePercentByte(b *strings.Builder, c byte) {
	b.WriteByte('%')
	b.WriteByte(upperHex[c>>4])
	b.WriteByte(upperHex[c&0x0f])
}
