package uris

// Package helpers includes a Go representation of the PKCS#11 URI scheme
// defined in RFC 7512 (https://datatracker.ietf.org/doc/html/rfc7512).
//
// !-> Note to reviewers <-!: Most of this is ai generated, but it has been reviewed and cross referenced with the rfc.
// Needed a slice of this, but thought we might as well have the full spec!
//
// PKCS11URI parses, serializes, and validates URIs of the form:
//
//	pkcs11:token=my-token;object=signing-key;type=private?pin-source=file:/etc/pin
//
// Build a URI to identify an HSM key:
//
//	id := []byte{0x01, 0x02, 0x03}
//	u := &uris.PKCS11URI{
//	    Token:  "my-token",
//	    Object: "signing-key",
//	    Type:   helpers.ObjectTypePrivate,
//	    ID:     id,
//	}
//	strUri := u.String() // "pkcs11:token=my-token;object=signing-key;type=private;id=%01%02%03"
//
// Parse one back from string:
//
//	u, err := uris.ParsePKCS11URI(strUri)
//
// Also great for resolving to a unique crypto asset on an hsm:
//
//	if err := p11.FindObjectsInit(session, u.ToAttributes()); err != nil {
//		return 0, err
//	}

import (
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/miekg/pkcs11"
	"github.com/samber/oops"
)

const ServiceName = "uris"

// pkcs11Scheme is the URI scheme prefix.
const pkcs11Scheme = "pkcs11:"

// ObjectType is the value of the "type" path attribute (CKA_CLASS).
type ObjectType string

const (
	ObjectTypePublic    ObjectType = "public"
	ObjectTypePrivate   ObjectType = "private"
	ObjectTypeCert      ObjectType = "cert"
	ObjectTypeSecretKey ObjectType = "secret-key"
	ObjectTypeData      ObjectType = "data"
)

// Valid reports whether t is the empty string or one of the values enumerated by RFC 7512.
func (t ObjectType) Valid() bool {
	switch t {
	case "", ObjectTypePublic, ObjectTypePrivate, ObjectTypeCert,
		ObjectTypeSecretKey, ObjectTypeData:
		return true
	}
	return false
}

// VendorAttr is a vendor-specific attribute. Names match `[A-Za-z0-9_-]+`.
// Per RFC 7512 §2.3, vendor-specific attributes may appear multiple times.
type VendorAttr struct {
	Name  string
	Value string
}

// PKCS11URI is a parsed PKCS#11 URI.
//
// String fields hold decoded UTF-8 text (no percent-encoding). Empty string,
// nil ID, and nil SlotID mean "attribute not set" and are omitted at
// serialization. The "id" path attribute is binary; it is encoded as raw bytes
// regardless of textual content.
type PKCS11URI struct {
	// Path attributes (RFC 7512 §2.3).
	Token               string     // CK_TOKEN_INFO.label
	Manufacturer        string     // CK_TOKEN_INFO.manufacturerID
	Serial              string     // CK_TOKEN_INFO.serialNumber
	Model               string     // CK_TOKEN_INFO.model
	LibraryManufacturer string     // CK_INFO.manufacturerID
	LibraryDescription  string     // CK_INFO.libraryDescription
	LibraryVersion      string     // CK_INFO.libraryVersion: "M" or "M.N", each 0-255
	Object              string     // CKA_LABEL
	Type                ObjectType // CKA_CLASS
	ID                  []byte     // CKA_ID (binary)
	SlotManufacturer    string     // CK_SLOT_INFO.manufacturerID
	SlotDescription     string     // CK_SLOT_INFO.slotDescription
	SlotID              *uint64    // CK_SLOT_ID (Cryptoki-assigned)
	VendorPath          []VendorAttr

	// Query attributes (RFC 7512 §2.4).
	PinSource   string // URI/command pointing at the PIN
	PinValue    string // Plaintext PIN (mutually exclusive with PinSource)
	ModuleName  string // Module name without system affixes
	ModulePath  string // Absolute filesystem path (mutually exclusive with ModuleName)
	VendorQuery []VendorAttr
}

// ParsePKCS11URI parses s as a PKCS#11 URI per RFC 7512.
func ParsePKCS11URI(s string) (*PKCS11URI, error) {
	if !strings.HasPrefix(s, pkcs11Scheme) {
		return nil, oops.In(ServiceName).Errorf("pkcs11uri: missing %q scheme prefix", pkcs11Scheme)
	}
	rest := s[len(pkcs11Scheme):]

	var pathPart, queryPart string
	hasQuery := false
	if before, after, ok := strings.Cut(rest, "?"); ok {
		pathPart, queryPart = before, after
		hasQuery = true
	} else {
		pathPart = rest
	}

	u := &PKCS11URI{}

	if pathPart != "" {
		seen := map[string]bool{}
		for _, raw := range strings.Split(pathPart, ";") {
			name, value, err := splitAttr(raw)
			if err != nil {
				return nil, err
			}
			if !isStandardPathAttr(name) {
				if !isVendorName(name) {
					return nil, oops.In(ServiceName).Errorf("pkcs11uri: invalid path attribute name %q", name)
				}
				dec, err := decodePathString(value)
				if err != nil {
					return nil, oops.In(ServiceName).Wrapf(err, "pkcs11uri: vendor attribute %q", name)
				}
				u.VendorPath = append(u.VendorPath, VendorAttr{Name: name, Value: dec})
				continue
			}
			if seen[name] {
				return nil, oops.In(ServiceName).Errorf("pkcs11uri: duplicate path attribute %q", name)
			}
			seen[name] = true
			if err := u.setPathAttr(name, value); err != nil {
				return nil, err
			}
		}
	}

	if hasQuery {
		seen := map[string]bool{}
		for _, raw := range strings.Split(queryPart, "&") {
			name, value, err := splitAttr(raw)
			if err != nil {
				return nil, err
			}
			if !isStandardQueryAttr(name) {
				if !isVendorName(name) {
					return nil, oops.In(ServiceName).Errorf("pkcs11uri: invalid query attribute name %q", name)
				}
				dec, err := decodeQueryString(value)
				if err != nil {
					return nil, oops.In(ServiceName).Wrapf(err, "pkcs11uri: vendor attribute %q", name)
				}
				u.VendorQuery = append(u.VendorQuery, VendorAttr{Name: name, Value: dec})
				continue
			}
			if seen[name] {
				return nil, oops.In(ServiceName).Errorf("pkcs11uri: duplicate query attribute %q", name)
			}
			seen[name] = true
			if err := u.setQueryAttr(name, value); err != nil {
				return nil, err
			}
		}
	}

	if err := u.Validate(); err != nil {
		return nil, err
	}
	return u, nil
}

// MustParsePKCS11URI is like ParsePKCS11URI but panics on error. Useful for
// constants and tests where the URI is known-valid at compile time.
func MustParsePKCS11URI(s string) *PKCS11URI {
	u, err := ParsePKCS11URI(s)
	if err != nil {
		panic(err)
	}
	return u
}

// String serializes the URI per RFC 7512. The output is canonical: path
// attributes appear in the order defined by the RFC, followed by vendor path
// attributes in insertion order; query attributes follow the same convention.
//
// String does not call Validate; serialize an invalid struct and you get an
// invalid string. Call Validate first if you need to be sure.
func (u *PKCS11URI) String() string {
	if u == nil {
		return pkcs11Scheme
	}
	var b strings.Builder
	b.WriteString(pkcs11Scheme)

	first := true
	addPath := func(name, encoded string) {
		if !first {
			b.WriteByte(';')
		}
		first = false
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(encoded)
	}
	addPathStr := func(name, value string) {
		if value == "" {
			return
		}
		addPath(name, encodePathString(value))
	}

	addPathStr("token", u.Token)
	addPathStr("manufacturer", u.Manufacturer)
	addPathStr("serial", u.Serial)
	addPathStr("model", u.Model)
	addPathStr("library-manufacturer", u.LibraryManufacturer)
	addPathStr("library-description", u.LibraryDescription)
	if u.LibraryVersion != "" {
		addPath("library-version", u.LibraryVersion)
	}
	addPathStr("object", u.Object)
	if u.Type != "" {
		addPath("type", string(u.Type))
	}
	if len(u.ID) > 0 {
		addPath("id", encodeIDBytes(u.ID))
	}
	addPathStr("slot-manufacturer", u.SlotManufacturer)
	addPathStr("slot-description", u.SlotDescription)
	if u.SlotID != nil {
		addPath("slot-id", strconv.FormatUint(*u.SlotID, 10))
	}
	for _, va := range u.VendorPath {
		addPath(va.Name, encodePathString(va.Value))
	}

	queryParts := u.queryParts()
	if len(queryParts) > 0 {
		b.WriteByte('?')
		for i, p := range queryParts {
			if i > 0 {
				b.WriteByte('&')
			}
			b.WriteString(p)
		}
	}
	return b.String()
}

func (u *PKCS11URI) queryParts() []string {
	var out []string
	addStr := func(name, value string) {
		if value == "" {
			return
		}
		out = append(out, name+"="+encodeQueryString(value))
	}
	addStr("pin-source", u.PinSource)
	addStr("pin-value", u.PinValue)
	addStr("module-name", u.ModuleName)
	addStr("module-path", u.ModulePath)
	for _, va := range u.VendorQuery {
		out = append(out, va.Name+"="+encodeQueryString(va.Value))
	}
	return out
}

// Validate checks the URI against RFC 7512 semantic constraints:
// the type enum, library-version format, mutually-exclusive attribute pairs,
// absolute module-path, and vendor name syntax.
func (u *PKCS11URI) Validate() error {
	if !u.Type.Valid() {
		return oops.In(ServiceName).Errorf("pkcs11uri: invalid type %q", u.Type)
	}
	if u.LibraryVersion != "" {
		if err := validateLibraryVersion(u.LibraryVersion); err != nil {
			return err
		}
	}
	if u.PinSource != "" && u.PinValue != "" {
		return oops.In(ServiceName).New("pkcs11uri: pin-source and pin-value are mutually exclusive")
	}
	if u.ModuleName != "" && u.ModulePath != "" {
		return oops.In(ServiceName).New("pkcs11uri: module-name and module-path are mutually exclusive")
	}
	if u.ModulePath != "" && !strings.HasPrefix(u.ModulePath, "/") {
		return oops.In(ServiceName).Errorf("pkcs11uri: module-path must be absolute, got %q", u.ModulePath)
	}
	for _, va := range u.VendorPath {
		if !isVendorName(va.Name) {
			return oops.In(ServiceName).Errorf("pkcs11uri: invalid vendor path attribute name %q", va.Name)
		}
		if isStandardPathAttr(va.Name) {
			return oops.In(ServiceName).Errorf("pkcs11uri: vendor path attribute %q shadows a standard attribute", va.Name)
		}
	}
	for _, va := range u.VendorQuery {
		if !isVendorName(va.Name) {
			return oops.In(ServiceName).Errorf("pkcs11uri: invalid vendor query attribute name %q", va.Name)
		}
		if isStandardQueryAttr(va.Name) {
			return oops.In(ServiceName).Errorf("pkcs11uri: vendor query attribute %q shadows a standard attribute", va.Name)
		}
	}
	return nil
}

// ToAttributes returns the object-level PKCS#11 attributes from the URI,
// suitable for passing to FindObjectsInit as a search template.
//
// Only the URI fields that map to PKCS#11 object attributes are emitted:
//
//	Object → CKA_LABEL
//	Type   → CKA_CLASS (CKO_PUBLIC_KEY, CKO_PRIVATE_KEY, CKO_CERTIFICATE,
//	         CKO_SECRET_KEY, CKO_DATA)
//	ID     → CKA_ID
//
// Token, slot, and library identifiers are not object attributes; the caller
// is responsible for using those fields to pick the right slot/session.
func (u *PKCS11URI) ToAttributes() []*pkcs11.Attribute {
	var attrs []*pkcs11.Attribute
	if u.Object != "" {
		attrs = append(attrs, pkcs11.NewAttribute(pkcs11.CKA_LABEL, u.Object))
	}
	if u.Type != "" {
		if class, ok := objectTypeToClass(u.Type); ok {
			attrs = append(attrs, pkcs11.NewAttribute(pkcs11.CKA_CLASS, class))
		}
	}
	if len(u.ID) > 0 {
		attrs = append(attrs, pkcs11.NewAttribute(pkcs11.CKA_ID, u.ID))
	}
	return attrs
}

// FromAttributes populates the object-level URI fields (Object, Type, ID)
// from a PKCS#11 attribute list, typically the result of GetAttributeValue.
// CKA_LABEL → Object, CKA_CLASS → Type, CKA_ID → ID. Other attribute types
// are ignored. Token/slot/library URI fields are left untouched.
func (u *PKCS11URI) FromAttributes(attrs []*pkcs11.Attribute) error {
	for _, a := range attrs {
		if a == nil {
			continue
		}
		switch a.Type {
		case pkcs11.CKA_LABEL:
			u.Object = string(a.Value)
		case pkcs11.CKA_ID:
			u.ID = append([]byte(nil), a.Value...)
		case pkcs11.CKA_CLASS:
			class, err := decodeCKULong(a.Value)
			if err != nil {
				return oops.In(ServiceName).Wrapf(err, "pkcs11uri: CKA_CLASS")
			}
			t, ok := classToObjectType(class)
			if !ok {
				return oops.In(ServiceName).Errorf("pkcs11uri: unsupported CKA_CLASS value %d", class)
			}
			u.Type = t
		}
	}
	return nil
}

// IDAttr returns a single attribute that uniquely identifies the key for an
// HSM lookup. CKA_ID is preferred when set, with CKA_LABEL as a fallback.
// Returns an error if neither ID nor Object is populated, since neither
// alone is enough to find a specific key.
func (u *PKCS11URI) IDAttr() (*pkcs11.Attribute, error) {
	if len(u.ID) > 0 {
		return pkcs11.NewAttribute(pkcs11.CKA_ID, u.ID), nil
	}
	if u.Object != "" {
		return pkcs11.NewAttribute(pkcs11.CKA_LABEL, u.Object), nil
	}
	return nil, oops.In(ServiceName).New("pkcs11uri: no uniquely identifying attribute set (need id or object)")
}

func objectTypeToClass(t ObjectType) (uint, bool) {
	switch t {
	case ObjectTypePublic:
		return pkcs11.CKO_PUBLIC_KEY, true
	case ObjectTypePrivate:
		return pkcs11.CKO_PRIVATE_KEY, true
	case ObjectTypeCert:
		return pkcs11.CKO_CERTIFICATE, true
	case ObjectTypeSecretKey:
		return pkcs11.CKO_SECRET_KEY, true
	case ObjectTypeData:
		return pkcs11.CKO_DATA, true
	}
	return 0, false
}

func classToObjectType(class uint64) (ObjectType, bool) {
	switch class {
	case uint64(pkcs11.CKO_PUBLIC_KEY):
		return ObjectTypePublic, true
	case uint64(pkcs11.CKO_PRIVATE_KEY):
		return ObjectTypePrivate, true
	case uint64(pkcs11.CKO_CERTIFICATE):
		return ObjectTypeCert, true
	case uint64(pkcs11.CKO_SECRET_KEY):
		return ObjectTypeSecretKey, true
	case uint64(pkcs11.CKO_DATA):
		return ObjectTypeData, true
	}
	return "", false
}

// decodeCKULong reads a CK_ULONG written by the HSM in native byte order.
// Linux/macOS on x86_64 and arm64 are little-endian; CK_ULONG is 8 bytes on
// modern 64-bit builds and 4 on 32-bit / Windows LLP64.
func decodeCKULong(b []byte) (uint64, error) {
	switch len(b) {
	case 8:
		return binary.LittleEndian.Uint64(b), nil
	case 4:
		return uint64(binary.LittleEndian.Uint32(b)), nil
	}
	return 0, oops.In(ServiceName).Errorf("expected 4 or 8 bytes for CK_ULONG, got %d", len(b))
}

// MarshalText implements encoding.TextMarshaler.
func (u *PKCS11URI) MarshalText() ([]byte, error) {
	if err := u.Validate(); err != nil {
		return nil, err
	}
	return []byte(u.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (u *PKCS11URI) UnmarshalText(b []byte) error {
	parsed, err := ParsePKCS11URI(string(b))
	if err != nil {
		return err
	}
	*u = *parsed
	return nil
}

// --- attribute application -------------------------------------------------

func (u *PKCS11URI) setPathAttr(name, value string) error {
	switch name {
	case "token":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.Token = v
	case "manufacturer":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.Manufacturer = v
	case "serial":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.Serial = v
	case "model":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.Model = v
	case "library-manufacturer":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.LibraryManufacturer = v
	case "library-description":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.LibraryDescription = v
	case "library-version":
		if err := validateLibraryVersion(value); err != nil {
			return err
		}
		u.LibraryVersion = value
	case "object":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.Object = v
	case "type":
		t := ObjectType(value)
		if !t.Valid() || t == "" {
			return oops.In(ServiceName).Errorf("pkcs11uri: invalid type %q", value)
		}
		u.Type = t
	case "id":
		v, err := decodePathBytes(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.ID = v
	case "slot-manufacturer":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.SlotManufacturer = v
	case "slot-description":
		v, err := decodePathString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.SlotDescription = v
	case "slot-id":
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return oops.In(ServiceName).Wrapf(err, "pkcs11uri: slot-id %q", value)
		}
		u.SlotID = &n
	default:
		return oops.In(ServiceName).Errorf("pkcs11uri: unknown standard path attribute %q", name)
	}
	return nil
}

func (u *PKCS11URI) setQueryAttr(name, value string) error {
	switch name {
	case "pin-source":
		v, err := decodeQueryString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.PinSource = v
	case "pin-value":
		v, err := decodeQueryString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.PinValue = v
	case "module-name":
		v, err := decodeQueryString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.ModuleName = v
	case "module-path":
		v, err := decodeQueryString(value)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.ModulePath = v
	default:
		return oops.In(ServiceName).Errorf("pkcs11uri: unknown standard query attribute %q", name)
	}
	return nil
}

// --- name predicates -------------------------------------------------------

func isStandardPathAttr(name string) bool {
	switch name {
	case "token", "manufacturer", "serial", "model",
		"library-manufacturer", "library-description", "library-version",
		"object", "type", "id",
		"slot-manufacturer", "slot-description", "slot-id":
		return true
	}
	return false
}

func isStandardQueryAttr(name string) bool {
	switch name {
	case "pin-source", "pin-value", "module-name", "module-path":
		return true
	}
	return false
}

// isVendorName checks the pk11-v-attr-nm-char rule: 1*(ALPHA / DIGIT / "-" / "_").
func isVendorName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// --- parsing helpers -------------------------------------------------------

func splitAttr(raw string) (name, value string, err error) {
	before, after, ok := strings.Cut(raw, "=")
	if !ok {
		return "", "", oops.In(ServiceName).Errorf("pkcs11uri: malformed attribute %q (missing '=')", raw)
	}
	name = before
	value = after
	if name == "" {
		return "", "", oops.In(ServiceName).Errorf("pkcs11uri: empty attribute name in %q", raw)
	}
	return name, value, nil
}

func validateLibraryVersion(v string) error {
	parts := strings.SplitN(v, ".", 2)
	for _, p := range parts {
		if p == "" {
			return oops.In(ServiceName).Errorf("pkcs11uri: invalid library-version %q", v)
		}
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return oops.In(ServiceName).Wrapf(err, "pkcs11uri: invalid library-version %q", v)
		}
		if n > 255 {
			return oops.In(ServiceName).Errorf("pkcs11uri: library-version component out of range 0-255 in %q", v)
		}
	}
	return nil
}

func wrapAttr(name string, err error) error {
	return oops.In(ServiceName).Wrapf(err, "pkcs11uri: attribute %q", name)
}

// --- percent-encoding ------------------------------------------------------

// isUnreserved reports whether c is in the RFC 3986 unreserved set.
func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

// pathSafeExtras is pk11-path-res-avail (pk11-res-avail + "&").
const pathSafeExtras = ":[]@!$'()*+,=&"

// querySafeExtras is pk11-query-res-avail (pk11-res-avail + "/" + "?" + "|").
const querySafeExtras = ":[]@!$'()*+,=/?|"

func isPathSafe(c byte) bool {
	return isUnreserved(c) || strings.IndexByte(pathSafeExtras, c) >= 0
}

func isQuerySafe(c byte) bool {
	return isUnreserved(c) || strings.IndexByte(querySafeExtras, c) >= 0
}

// encodePathString percent-encodes s for the path component.
func encodePathString(s string) string { return percentEncode(s, isPathSafe) }

// encodeQueryString percent-encodes s for the query component.
func encodeQueryString(s string) string { return percentEncode(s, isQuerySafe) }

// encodeIDBytes encodes every byte as %XX. The id attribute is binary;
// matching against CKA_ID is byte-for-byte and the RFC examples encode the
// whole value, so we do too.
func encodeIDBytes(b []byte) string {
	out := make([]byte, 0, 3*len(b))
	for _, c := range b {
		out = append(out, '%', hexDigit(c>>4), hexDigit(c&0xf))
	}
	return string(out)
}

func percentEncode(s string, safe func(byte) bool) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if safe(c) {
			out = append(out, c)
			continue
		}
		out = append(out, '%', hexDigit(c>>4), hexDigit(c&0xf))
	}
	return string(out)
}

func hexDigit(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + n - 10
}

// decodePathString percent-decodes s and validates its characters per pk11-pchar.
func decodePathString(s string) (string, error) {
	b, err := decodePathBytes(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeQueryString percent-decodes s and validates its characters per pk11-qchar.
func decodeQueryString(s string) (string, error) {
	b, err := percentDecode(s, isQuerySafe)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodePathBytes(s string) ([]byte, error) {
	return percentDecode(s, isPathSafe)
}

func percentDecode(s string, safe func(byte) bool) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '%':
			if i+2 >= len(s) {
				return nil, oops.In(ServiceName).Errorf("truncated percent-encoding at offset %d", i)
			}
			hi, e1 := hexValue(s[i+1])
			lo, e2 := hexValue(s[i+2])
			if e1 != nil || e2 != nil {
				return nil, oops.In(ServiceName).Errorf("invalid percent-encoding %q", s[i:i+3])
			}
			out = append(out, hi<<4|lo)
			i += 2
		case safe(c):
			out = append(out, c)
		default:
			return nil, oops.In(ServiceName).Errorf("disallowed character %q at offset %d", c, i)
		}
	}
	return out, nil
}

func hexValue(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, oops.In(ServiceName).Errorf("invalid hex digit %q", c)
}
