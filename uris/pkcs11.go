package uris

import (
	"encoding/binary"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/miekg/pkcs11"
	"github.com/samber/oops"
)

const (
	ServiceName  = "uris"
	pkcs11Scheme = "pkcs11:"
)

// ObjectType is the RFC 7512 value of the "type" path attribute.
type ObjectType string

const (
	ObjectTypePublic    ObjectType = "public"
	ObjectTypePrivate   ObjectType = "private"
	ObjectTypeCert      ObjectType = "cert"
	ObjectTypeSecretKey ObjectType = "secret-key"
	ObjectTypeData      ObjectType = "data"
)

// Valid reports whether t is empty or one of the object types defined by
// RFC 7512. Empty is valid for an unset PKCS11URI.Type field.
func (t ObjectType) Valid() bool {
	switch t {
	case "", ObjectTypePublic, ObjectTypePrivate, ObjectTypeCert, ObjectTypeSecretKey, ObjectTypeData:
		return true
	default:
		return false
	}
}

// VendorAttr is a vendor-specific RFC 7512 attribute.
type VendorAttr struct {
	Name  string
	Value string
}

// PKCS11URI is a decoded PKCS #11 URI as defined by RFC 7512.
//
// Text fields contain decoded UTF-8. ID contains the raw CKA_ID bytes. Most
// zero-value fields mean "attribute absent". When parsing, the implementation
// also remembers whether an otherwise-empty standard attribute was present,
// so values such as "serial=" and "id=" round-trip without changing their
// matching semantics.
type PKCS11URI struct {
	// Path attributes (RFC 7512 section 2.3).
	Token               string
	Manufacturer        string
	Serial              string
	Model               string
	LibraryManufacturer string
	LibraryDescription  string
	LibraryVersion      string
	Object              string
	Type                ObjectType
	ID                  []byte
	SlotManufacturer    string
	SlotDescription     string
	SlotID              *uint64
	VendorPath          []VendorAttr

	// Query attributes (RFC 7512 section 2.4).
	PinSource   string
	PinValue    string
	ModuleName  string
	ModulePath  string
	VendorQuery []VendorAttr

	// present preserves parsed empty standard attributes without changing the
	// public struct-literal API.
	present uint32
}

// ParsePKCS11URI parses and validates an RFC 7512 PKCS #11 URI.
func ParsePKCS11URI(s string) (*PKCS11URI, error) {
	if len(s) < len(pkcs11Scheme) || !strings.EqualFold(s[:len(pkcs11Scheme)], pkcs11Scheme) {
		return nil, uriErrorf("missing %q scheme prefix", pkcs11Scheme)
	}

	rest := s[len(pkcs11Scheme):]
	pathPart, queryPart, hasQuery := strings.Cut(rest, "?")
	u := &PKCS11URI{}

	if pathPart != "" {
		seen := make(map[string]struct{})
		for raw := range strings.SplitSeq(pathPart, ";") {
			rawName, value, err := splitAttr(raw)
			if err != nil {
				return nil, err
			}

			if name, ok := canonicalPathAttr(rawName); ok {
				key := "standard:" + name
				if _, duplicate := seen[key]; duplicate {
					return nil, uriErrorf("duplicate path attribute %q", rawName)
				}
				seen[key] = struct{}{}
				if err := u.setPathAttr(name, value); err != nil {
					return nil, err
				}
				continue
			}

			if isStandardAttr(rawName) {
				return nil, uriErrorf("vendor path attribute %q clashes with a standard attribute", rawName)
			}
			if !isVendorName(rawName) {
				return nil, uriErrorf("invalid path attribute name %q", rawName)
			}
			key := "vendor:" + rawName
			if _, duplicate := seen[key]; duplicate {
				return nil, uriErrorf("duplicate path attribute %q", rawName)
			}
			seen[key] = struct{}{}

			decoded, err := decodePathString(value)
			if err != nil {
				return nil, wrapAttr(rawName, err)
			}
			u.VendorPath = append(u.VendorPath, VendorAttr{Name: rawName, Value: decoded})
		}
	}

	// RFC 7512 explicitly allows an empty query component.
	if hasQuery && queryPart != "" {
		seenStandard := make(map[string]struct{})
		for raw := range strings.SplitSeq(queryPart, "&") {
			rawName, value, err := splitAttr(raw)
			if err != nil {
				return nil, err
			}

			if name, ok := canonicalQueryAttr(rawName); ok {
				if _, duplicate := seenStandard[name]; duplicate {
					return nil, uriErrorf("duplicate query attribute %q", rawName)
				}
				seenStandard[name] = struct{}{}
				if err := u.setQueryAttr(name, value); err != nil {
					return nil, err
				}
				continue
			}

			if isStandardAttr(rawName) {
				return nil, uriErrorf("vendor query attribute %q clashes with a standard attribute", rawName)
			}
			if !isVendorName(rawName) {
				return nil, uriErrorf("invalid query attribute name %q", rawName)
			}

			decoded, err := decodeQueryString(value)
			if err != nil {
				return nil, wrapAttr(rawName, err)
			}
			// Duplicate vendor query attributes are explicitly permitted by RFC 7512.
			u.VendorQuery = append(u.VendorQuery, VendorAttr{Name: rawName, Value: decoded})
		}
	}

	if err := u.Validate(); err != nil {
		return nil, err
	}
	return u, nil
}

// MustParsePKCS11URI is like ParsePKCS11URI but panics on error.
func MustParsePKCS11URI(s string) *PKCS11URI {
	u, err := ParsePKCS11URI(s)
	if err != nil {
		panic(err)
	}
	return u
}

// Has reports whether a standard attribute is present. It is primarily useful
// for distinguishing a parsed empty value (for example, "serial=") from an
// absent attribute.
func (u *PKCS11URI) Has(name string) bool {
	if u == nil {
		return false
	}
	if canonical, ok := canonicalPathAttr(name); ok {
		return u.hasStandard(canonical)
	}
	if canonical, ok := canonicalQueryAttr(name); ok {
		return u.hasStandard(canonical)
	}
	return false
}

// String serializes u using deterministic RFC 7512 attribute ordering and
// uppercase percent escapes. It does not validate programmatically-constructed
// values; use MarshalText or Validate when validation is required.
func (u *PKCS11URI) String() string {
	if u == nil {
		return pkcs11Scheme
	}

	path := make([]string, 0, 13+len(u.VendorPath))
	addPathText := func(name, value string) {
		if value != "" || u.hasStandard(name) {
			path = append(path, name+"="+encodePathString(value))
		}
	}

	addPathText("token", u.Token)
	addPathText("manufacturer", u.Manufacturer)
	addPathText("serial", u.Serial)
	addPathText("model", u.Model)
	addPathText("library-manufacturer", u.LibraryManufacturer)
	addPathText("library-description", u.LibraryDescription)
	if u.LibraryVersion != "" {
		path = append(path, "library-version="+u.LibraryVersion)
	}
	addPathText("object", u.Object)
	if u.Type != "" {
		path = append(path, "type="+string(u.Type))
	}
	if len(u.ID) != 0 || u.hasStandard("id") {
		path = append(path, "id="+encodeIDBytes(u.ID))
	}
	addPathText("slot-manufacturer", u.SlotManufacturer)
	addPathText("slot-description", u.SlotDescription)
	if u.SlotID != nil {
		path = append(path, "slot-id="+strconv.FormatUint(*u.SlotID, 10))
	}
	for _, attr := range u.VendorPath {
		path = append(path, attr.Name+"="+encodePathString(attr.Value))
	}

	out := pkcs11Scheme + strings.Join(path, ";")
	query := u.queryParts()
	if len(query) != 0 {
		out += "?" + strings.Join(query, "&")
	}
	return out
}

func (u *PKCS11URI) queryParts() []string {
	query := make([]string, 0, 4+len(u.VendorQuery))
	add := func(name, value string) {
		if value != "" || u.hasStandard(name) {
			query = append(query, name+"="+encodeQueryString(value))
		}
	}

	add("pin-source", u.PinSource)
	add("pin-value", u.PinValue)
	add("module-name", u.ModuleName)
	add("module-path", u.ModulePath)
	for _, attr := range u.VendorQuery {
		query = append(query, attr.Name+"="+encodeQueryString(attr.Value))
	}
	return query
}

// Validate checks semantic constraints enforced by this package.
func (u *PKCS11URI) Validate() error {
	if u == nil {
		return uriErrorf("nil PKCS11URI")
	}
	if !u.Type.Valid() {
		return uriErrorf("invalid type %q", u.Type)
	}
	if u.LibraryVersion != "" {
		if err := validateLibraryVersion(u.LibraryVersion); err != nil {
			return err
		}
	}

	// RFC 7512 recommends rejecting URIs containing both PIN attributes.
	if u.Has("pin-source") && u.Has("pin-value") {
		return uriErrorf("pin-source and pin-value are mutually exclusive")
	}
	// Using module-name and module-path together is discouraged by RFC 7512,
	// but explicitly has implementation-specific semantics, so it is allowed.
	if u.Has("module-path") && !filepath.IsAbs(u.ModulePath) {
		return uriErrorf("module-path must be absolute, got %q", u.ModulePath)
	}

	for name, value := range u.textFields() {
		if !utf8.ValidString(value) {
			return uriErrorf("attribute %q is not valid UTF-8", name)
		}
	}

	seenVendorPath := make(map[string]struct{}, len(u.VendorPath))
	for _, attr := range u.VendorPath {
		if err := validateVendorAttr("path", attr); err != nil {
			return err
		}
		if _, duplicate := seenVendorPath[attr.Name]; duplicate {
			return uriErrorf("duplicate vendor path attribute %q", attr.Name)
		}
		seenVendorPath[attr.Name] = struct{}{}
	}
	for _, attr := range u.VendorQuery {
		if err := validateVendorAttr("query", attr); err != nil {
			return err
		}
	}

	return nil
}

func (u *PKCS11URI) textFields() map[string]string {
	return map[string]string{
		"token":                u.Token,
		"manufacturer":         u.Manufacturer,
		"serial":               u.Serial,
		"model":                u.Model,
		"library-manufacturer": u.LibraryManufacturer,
		"library-description":  u.LibraryDescription,
		"object":               u.Object,
		"slot-manufacturer":    u.SlotManufacturer,
		"slot-description":     u.SlotDescription,
		"pin-source":           u.PinSource,
		"pin-value":            u.PinValue,
		"module-name":          u.ModuleName,
		"module-path":          u.ModulePath,
	}
}

func validateVendorAttr(component string, attr VendorAttr) error {
	if !isVendorName(attr.Name) {
		return uriErrorf("invalid vendor %s attribute name %q", component, attr.Name)
	}
	if isStandardAttr(attr.Name) {
		return uriErrorf("vendor %s attribute %q clashes with a standard attribute", component, attr.Name)
	}
	if !utf8.ValidString(attr.Value) {
		return uriErrorf("vendor %s attribute %q is not valid UTF-8", component, attr.Name)
	}
	return nil
}

// ToAttributes returns the object-level attributes usable as a PKCS #11
// FindObjectsInit template. Token, slot, and library fields are intentionally
// excluded because they are not object attributes.
func (u *PKCS11URI) ToAttributes() []*pkcs11.Attribute {
	if u == nil {
		return nil
	}

	attrs := make([]*pkcs11.Attribute, 0, 3)
	if u.Has("object") {
		attrs = append(attrs, pkcs11.NewAttribute(pkcs11.CKA_LABEL, u.Object))
	}
	if class, ok := objectTypeToClass(u.Type); ok {
		attrs = append(attrs, pkcs11.NewAttribute(pkcs11.CKA_CLASS, class))
	}
	if u.Has("id") {
		attrs = append(attrs, pkcs11.NewAttribute(pkcs11.CKA_ID, u.ID))
	}
	return attrs
}

// FromAttributes populates Object, Type, and ID from PKCS #11 attributes.
// Other fields are left unchanged.
func (u *PKCS11URI) FromAttributes(attrs []*pkcs11.Attribute) error {
	if u == nil {
		return uriErrorf("nil PKCS11URI")
	}

	for _, attr := range attrs {
		if attr == nil {
			continue
		}
		switch attr.Type {
		case pkcs11.CKA_LABEL:
			u.Object = string(attr.Value)
			u.markPresent("object")
		case pkcs11.CKA_ID:
			u.ID = append(u.ID[:0], attr.Value...)
			u.markPresent("id")
		case pkcs11.CKA_CLASS:
			class, err := decodeCKULong(attr.Value)
			if err != nil {
				return oops.In(ServiceName).Wrapf(err, "pkcs11uri: CKA_CLASS")
			}
			t, ok := classToObjectType(class)
			if !ok {
				return uriErrorf("unsupported CKA_CLASS value %d", class)
			}
			u.Type = t
			u.markPresent("type")
		}
	}
	return nil
}

// IDAttr returns the preferred single lookup attribute: CKA_ID when present,
// otherwise CKA_LABEL. Neither attribute is guaranteed by PKCS #11 to be
// globally unique; callers that require uniqueness should add appropriate
// token/type constraints.
func (u *PKCS11URI) IDAttr() (*pkcs11.Attribute, error) {
	if u == nil {
		return nil, uriErrorf("nil PKCS11URI")
	}
	if u.Has("id") {
		return pkcs11.NewAttribute(pkcs11.CKA_ID, u.ID), nil
	}
	if u.Has("object") {
		return pkcs11.NewAttribute(pkcs11.CKA_LABEL, u.Object), nil
	}
	return nil, uriErrorf("no identifying object attribute set (need id or object)")
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
	default:
		return 0, false
	}
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
	default:
		return "", false
	}
}

// decodeCKULong decodes a CK_ULONG returned by the native PKCS #11 module.
func decodeCKULong(b []byte) (uint64, error) {
	switch len(b) {
	case 4:
		return uint64(binary.NativeEndian.Uint32(b)), nil
	case 8:
		return binary.NativeEndian.Uint64(b), nil
	default:
		return 0, uriErrorf("expected 4 or 8 bytes for CK_ULONG, got %d", len(b))
	}
}

// MarshalText implements encoding.TextMarshaler.
func (u *PKCS11URI) MarshalText() ([]byte, error) {
	if err := u.Validate(); err != nil {
		return nil, err
	}
	return []byte(u.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (u *PKCS11URI) UnmarshalText(text []byte) error {
	if u == nil {
		return uriErrorf("nil PKCS11URI")
	}
	parsed, err := ParsePKCS11URI(string(text))
	if err != nil {
		return err
	}
	*u = *parsed
	return nil
}

func (u *PKCS11URI) setPathAttr(name, raw string) error {
	switch name {
	case "library-version":
		if err := validateLibraryVersion(raw); err != nil {
			return err
		}
		u.LibraryVersion = raw
	case "type":
		t := ObjectType(raw)
		if t == "" || !t.Valid() {
			return uriErrorf("invalid type %q", raw)
		}
		u.Type = t
	case "id":
		value, err := decodePathBytes(raw)
		if err != nil {
			return wrapAttr(name, err)
		}
		u.ID = value
	case "slot-id":
		value, err := parseDecimalUint(raw, 64)
		if err != nil {
			return oops.In(ServiceName).Wrapf(err, "pkcs11uri: slot-id %q", raw)
		}
		u.SlotID = &value
	default:
		value, err := decodePathString(raw)
		if err != nil {
			return wrapAttr(name, err)
		}
		switch name {
		case "token":
			u.Token = value
		case "manufacturer":
			u.Manufacturer = value
		case "serial":
			u.Serial = value
		case "model":
			u.Model = value
		case "library-manufacturer":
			u.LibraryManufacturer = value
		case "library-description":
			u.LibraryDescription = value
		case "object":
			u.Object = value
		case "slot-manufacturer":
			u.SlotManufacturer = value
		case "slot-description":
			u.SlotDescription = value
		default:
			return uriErrorf("unknown standard path attribute %q", name)
		}
	}

	u.markPresent(name)
	return nil
}

func (u *PKCS11URI) setQueryAttr(name, raw string) error {
	value, err := decodeQueryString(raw)
	if err != nil {
		return wrapAttr(name, err)
	}

	switch name {
	case "pin-source":
		u.PinSource = value
	case "pin-value":
		u.PinValue = value
	case "module-name":
		u.ModuleName = value
	case "module-path":
		u.ModulePath = value
	default:
		return uriErrorf("unknown standard query attribute %q", name)
	}

	u.markPresent(name)
	return nil
}

func (u *PKCS11URI) markPresent(name string) {
	u.present |= standardPresenceBit(name)
}

func (u *PKCS11URI) hasStandard(name string) bool {
	if u == nil {
		return false
	}
	if bit := standardPresenceBit(name); bit != 0 && u.present&bit != 0 {
		return true
	}

	switch name {
	case "token":
		return u.Token != ""
	case "manufacturer":
		return u.Manufacturer != ""
	case "serial":
		return u.Serial != ""
	case "model":
		return u.Model != ""
	case "library-manufacturer":
		return u.LibraryManufacturer != ""
	case "library-description":
		return u.LibraryDescription != ""
	case "library-version":
		return u.LibraryVersion != ""
	case "object":
		return u.Object != ""
	case "type":
		return u.Type != ""
	case "id":
		return len(u.ID) != 0
	case "slot-manufacturer":
		return u.SlotManufacturer != ""
	case "slot-description":
		return u.SlotDescription != ""
	case "slot-id":
		return u.SlotID != nil
	case "pin-source":
		return u.PinSource != ""
	case "pin-value":
		return u.PinValue != ""
	case "module-name":
		return u.ModuleName != ""
	case "module-path":
		return u.ModulePath != ""
	default:
		return false
	}
}

func standardPresenceBit(name string) uint32 {
	switch name {
	case "token":
		return 1 << 0
	case "manufacturer":
		return 1 << 1
	case "serial":
		return 1 << 2
	case "model":
		return 1 << 3
	case "library-manufacturer":
		return 1 << 4
	case "library-description":
		return 1 << 5
	case "object":
		return 1 << 6
	case "id":
		return 1 << 7
	case "slot-manufacturer":
		return 1 << 8
	case "slot-description":
		return 1 << 9
	case "pin-source":
		return 1 << 10
	case "pin-value":
		return 1 << 11
	case "module-name":
		return 1 << 12
	case "module-path":
		return 1 << 13
	default:
		return 0
	}
}

func canonicalPathAttr(name string) (string, bool) {
	name = strings.ToLower(name)
	switch name {
	case "token", "manufacturer", "serial", "model",
		"library-manufacturer", "library-description", "library-version",
		"object", "type", "id", "slot-manufacturer", "slot-description", "slot-id":
		return name, true
	default:
		return "", false
	}
}

func canonicalQueryAttr(name string) (string, bool) {
	name = strings.ToLower(name)
	switch name {
	case "pin-source", "pin-value", "module-name", "module-path":
		return name, true
	default:
		return "", false
	}
}

func isStandardAttr(name string) bool {
	_, path := canonicalPathAttr(name)
	_, query := canonicalQueryAttr(name)
	return path || query
}

// isVendorName implements RFC 7512 pk11-v-attr-nm-char.
func isVendorName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func splitAttr(raw string) (name, value string, err error) {
	name, value, ok := strings.Cut(raw, "=")
	if !ok {
		return "", "", uriErrorf("malformed attribute %q (missing '=')", raw)
	}
	if name == "" {
		return "", "", uriErrorf("empty attribute name in %q", raw)
	}
	return name, value, nil
}

func validateLibraryVersion(v string) error {
	major, minor, hasMinor := strings.Cut(v, ".")
	if major == "" || hasMinor && minor == "" || strings.Contains(minor, ".") {
		return uriErrorf("invalid library-version %q", v)
	}
	if _, err := parseDecimalUint(major, 8); err != nil {
		return oops.In(ServiceName).Wrapf(err, "pkcs11uri: invalid library-version %q", v)
	}
	if hasMinor {
		if _, err := parseDecimalUint(minor, 8); err != nil {
			return oops.In(ServiceName).Wrapf(err, "pkcs11uri: invalid library-version %q", v)
		}
	}
	return nil
}

func parseDecimalUint(s string, bitSize int) (uint64, error) {
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, strconv.ErrSyntax
		}
	}
	return strconv.ParseUint(s, 10, bitSize)
}

func uriErrorf(format string, args ...any) error {
	return oops.In(ServiceName).Errorf("pkcs11uri: "+format, args...)
}

func wrapAttr(name string, err error) error {
	return oops.In(ServiceName).Wrapf(err, "pkcs11uri: attribute %q", name)
}

const (
	pathSafeExtras  = ":[]@!$'()*+,=&"
	querySafeExtras = ":[]@!$'()*+,=/?|"
)

func isPathSafe(c byte) bool {
	return isUnreserved(c) || strings.IndexByte(pathSafeExtras, c) >= 0
}

func isQuerySafe(c byte) bool {
	return isUnreserved(c) || strings.IndexByte(querySafeExtras, c) >= 0
}

func encodePathString(s string) string  { return percentEncode(s, isPathSafe) }
func encodeQueryString(s string) string { return percentEncode(s, isQuerySafe) }

func encodeIDBytes(id []byte) string {
	var b strings.Builder
	b.Grow(3 * len(id))
	for _, c := range id {
		writePercentByte(&b, c)
	}
	return b.String()
}

func decodePathString(s string) (string, error) {
	return decodeText(s, isPathSafe)
}

func decodeQueryString(s string) (string, error) {
	return decodeText(s, isQuerySafe)
}

func decodeText(s string, safe func(byte) bool) (string, error) {
	decoded, err := percentDecode(s, safe)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(decoded) {
		return "", uriErrorf("decoded value is not valid UTF-8")
	}
	return string(decoded), nil
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
				return nil, uriErrorf("truncated percent-encoding at offset %d", i)
			}
			hi, okHi := hexValue(s[i+1])
			lo, okLo := hexValue(s[i+2])
			if !okHi || !okLo {
				return nil, uriErrorf("invalid percent-encoding %q", s[i:i+3])
			}
			out = append(out, hi<<4|lo)
			i += 2
		case safe(c):
			out = append(out, c)
		default:
			return nil, uriErrorf("disallowed character %q at offset %d", c, i)
		}
	}
	return out, nil
}

func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}
