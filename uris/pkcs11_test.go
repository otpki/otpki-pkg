package uris

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/miekg/pkcs11"
)

func TestParsePKCS11URI_RFCExamples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "pkcs11:", "pkcs11:"},
		{"object", "pkcs11:object=my-pubkey;type=public", "pkcs11:object=my-pubkey;type=public"},
		{"private with pin source", "pkcs11:object=my-key;type=private?pin-source=file:/etc/token", "pkcs11:object=my-key;type=private?pin-source=file:/etc/token"},
		{
			"certificate with empty serial",
			"pkcs11:token=The%20Software%20PKCS%2311%20Softtoken;manufacturer=Snake%20Oil,%20Inc.;model=1.0;object=my-certificate;type=cert;id=%69%95%3E%5C%F4%BD%EC%91;serial=?pin-source=file:/etc/token_pin",
			"pkcs11:token=The%20Software%20PKCS%2311%20Softtoken;manufacturer=Snake%20Oil,%20Inc.;serial=;model=1.0;object=my-certificate;type=cert;id=%69%95%3E%5C%F4%BD%EC%91?pin-source=file:/etc/token_pin",
		},
		{"module name", "pkcs11:object=my-sign-key;type=private?module-name=mypkcs11", "pkcs11:object=my-sign-key;type=private?module-name=mypkcs11"},
		{"module path", "pkcs11:object=my-sign-key;type=private?module-path=/mnt/libmypkcs11.so.1", "pkcs11:object=my-sign-key;type=private?module-path=/mnt/libmypkcs11.so.1"},
		{"escaped percent semicolon", "pkcs11:token=A%20name%20with%20a%20substring%20%25%3B;object=my-certificate;type=cert", "pkcs11:token=A%20name%20with%20a%20substring%20%25%3B;object=my-certificate;type=cert"},
		{"unicode", "pkcs11:token=Name%20with%20%C3%A1;object=my-certificate;type=cert", "pkcs11:token=Name%20with%20%C3%A1;object=my-certificate;type=cert"},
		{"vendor attributes", "pkcs11:token=my-token;vendor-aaa=value-a?pin-source=file:/etc/token_pin&vendor-bbb=value-b", "pkcs11:token=my-token;vendor-aaa=value-a?pin-source=file:/etc/token_pin&vendor-bbb=value-b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u, err := ParsePKCS11URI(tt.in)
			if err != nil {
				t.Fatalf("ParsePKCS11URI(%q): %v", tt.in, err)
			}
			if got := u.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsePKCS11URI_AllFieldsRoundTrip(t *testing.T) {
	t.Parallel()

	in := "pkcs11:token=tok;manufacturer=maker;serial=123;model=m;library-manufacturer=libmaker;library-description=desc;library-version=3.7;object=key;type=private;id=%00%01%FE%FF;slot-manufacturer=slotmaker;slot-description=slot;slot-id=42;vendor_path=x?pin-source=file:/pin&module-name=mod&vendor_query=y"
	u, err := ParsePKCS11URI(in)
	if err != nil {
		t.Fatal(err)
	}

	if u.Token != "tok" || u.Manufacturer != "maker" || u.Serial != "123" || u.Model != "m" {
		t.Fatalf("unexpected token fields: %+v", u)
	}
	if u.LibraryManufacturer != "libmaker" || u.LibraryDescription != "desc" || u.LibraryVersion != "3.7" {
		t.Fatalf("unexpected library fields: %+v", u)
	}
	if u.Object != "key" || u.Type != ObjectTypePrivate || !bytes.Equal(u.ID, []byte{0, 1, 0xfe, 0xff}) {
		t.Fatalf("unexpected object fields: %+v", u)
	}
	if u.SlotID == nil || *u.SlotID != 42 || u.SlotManufacturer != "slotmaker" || u.SlotDescription != "slot" {
		t.Fatalf("unexpected slot fields: %+v", u)
	}
	if u.PinSource != "file:/pin" || u.ModuleName != "mod" {
		t.Fatalf("unexpected query fields: %+v", u)
	}
	if !reflect.DeepEqual(u.VendorPath, []VendorAttr{{Name: "vendor_path", Value: "x"}}) {
		t.Fatalf("VendorPath = %#v", u.VendorPath)
	}
	if !reflect.DeepEqual(u.VendorQuery, []VendorAttr{{Name: "vendor_query", Value: "y"}}) {
		t.Fatalf("VendorQuery = %#v", u.VendorQuery)
	}

	canonical := u.String()
	u2, err := ParsePKCS11URI(canonical)
	if err != nil {
		t.Fatalf("reparse %q: %v", canonical, err)
	}
	if got := u2.String(); got != canonical {
		t.Fatalf("second serialization = %q, want %q", got, canonical)
	}
}

func TestParsePKCS11URI_PreservesEmptyAttributes(t *testing.T) {
	t.Parallel()

	u, err := ParsePKCS11URI("pkcs11:token=;serial=;object=;id=?module-name=")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"token", "serial", "object", "id", "module-name"} {
		if !u.Has(name) {
			t.Errorf("Has(%q) = false, want true", name)
		}
	}
	if u.Has("manufacturer") {
		t.Error("Has(manufacturer) = true, want false")
	}

	want := "pkcs11:token=;serial=;object=;id=?module-name="
	if got := u.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestParsePKCS11URI_NormalizesIDPercentEscapes(t *testing.T) {
	t.Parallel()

	u, err := ParsePKCS11URI("pkcs11:id=%ba%ddabc")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.String(), "pkcs11:id=%BA%DD%61%62%63"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestPKCS11URICopyDoesNotSharePresenceState(t *testing.T) {
	t.Parallel()

	original, err := ParsePKCS11URI("pkcs11:serial=")
	if err != nil {
		t.Fatal(err)
	}
	clone := *original
	if err := clone.UnmarshalText([]byte("pkcs11:token=")); err != nil {
		t.Fatal(err)
	}
	if !original.Has("serial") || original.Has("token") {
		t.Fatalf("copy mutation changed original presence state: %q", original.String())
	}
}

func TestParsePKCS11URI_EmptyQueryIsValid(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"pkcs11:?", "pkcs11:token=x?"} {
		u, err := ParsePKCS11URI(in)
		if err != nil {
			t.Fatalf("ParsePKCS11URI(%q): %v", in, err)
		}
		if strings.HasSuffix(u.String(), "?") {
			t.Fatalf("canonical String() unexpectedly retained empty query: %q", u.String())
		}
	}
}

func TestParsePKCS11URI_StandardNamesAreCaseInsensitive(t *testing.T) {
	t.Parallel()

	u, err := ParsePKCS11URI("PKCS11:TOKEN=abc;OBJECT=key;TYPE=private?MODULE-NAME=mod")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.String(), "pkcs11:token=abc;object=key;type=private?module-name=mod"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestParsePKCS11URI_VendorQueryDuplicatesAllowed(t *testing.T) {
	t.Parallel()

	u, err := ParsePKCS11URI("pkcs11:object=k?vendor=x&vendor=y")
	if err != nil {
		t.Fatal(err)
	}
	want := []VendorAttr{{Name: "vendor", Value: "x"}, {Name: "vendor", Value: "y"}}
	if !reflect.DeepEqual(u.VendorQuery, want) {
		t.Fatalf("VendorQuery = %#v, want %#v", u.VendorQuery, want)
	}
	if got := u.String(); got != "pkcs11:object=k?vendor=x&vendor=y" {
		t.Fatalf("String() = %q", got)
	}
}

func TestParsePKCS11URI_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
		msg  string
	}{
		{"wrong scheme", "http:token=x", "scheme"},
		{"missing equals", "pkcs11:token", "missing '='"},
		{"empty path name", "pkcs11:=x", "empty attribute name"},
		{"empty query name", "pkcs11:token=x?=y", "empty attribute name"},
		{"trailing path separator", "pkcs11:token=x;", "missing '='"},
		{"trailing query separator", "pkcs11:token=x?x=y&", "missing '='"},
		{"duplicate standard path", "pkcs11:token=a;TOKEN=b", "duplicate path"},
		{"duplicate vendor path", "pkcs11:vendor=a;vendor=b", "duplicate path"},
		{"duplicate standard query", "pkcs11:?module-name=a&MODULE-NAME=b", "duplicate query"},
		{"standard query name used as vendor path", "pkcs11:pin-value=x", "clashes"},
		{"standard path name used as vendor query", "pkcs11:?token=x", "clashes"},
		{"invalid vendor path name", "pkcs11:bad.name=x", "invalid path attribute name"},
		{"invalid vendor query name", "pkcs11:?bad.name=x", "invalid query attribute name"},
		{"invalid type", "pkcs11:type=key", "invalid type"},
		{"empty type", "pkcs11:type=", "invalid type"},
		{"version empty minor", "pkcs11:library-version=1.", "library-version"},
		{"version empty major", "pkcs11:library-version=.1", "library-version"},
		{"version too many dots", "pkcs11:library-version=1.2.3", "library-version"},
		{"version major plus", "pkcs11:library-version=+1", "library-version"},
		{"version minor plus", "pkcs11:library-version=1.+2", "library-version"},
		{"version major range", "pkcs11:library-version=256", "library-version"},
		{"version minor range", "pkcs11:library-version=1.256", "library-version"},
		{"slot plus", "pkcs11:slot-id=+1", "slot-id"},
		{"slot negative", "pkcs11:slot-id=-1", "slot-id"},
		{"slot empty", "pkcs11:slot-id=", "slot-id"},
		{"path slash", "pkcs11:token=a/b", "disallowed character"},
		{"path fragment", "pkcs11:token=a#b", "disallowed character"},
		{"path semicolon", "pkcs11:token=a;b", "missing '='"},
		{"query ampersand", "pkcs11:?pin-source=a&b", "missing '='"},
		{"bad percent", "pkcs11:token=%zz", "percent-encoding"},
		{"truncated percent", "pkcs11:token=%A", "percent-encoding"},
		{"invalid utf8", "pkcs11:token=%FF", "UTF-8"},
		{"pin source and value", "pkcs11:?pin-source=&pin-value=x", "mutually exclusive"},
		{"relative module path", "pkcs11:?module-path=relative.so", "must be absolute"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParsePKCS11URI(tt.uri)
			if err == nil {
				t.Fatalf("ParsePKCS11URI(%q) succeeded, want error", tt.uri)
			}
			if !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("error %q does not contain %q", err, tt.msg)
			}
		})
	}
}

func TestPKCS11URI_ModuleNameAndPathMayCoexist(t *testing.T) {
	t.Parallel()

	u := &PKCS11URI{ModuleName: "mypkcs11", ModulePath: "/usr/lib/libmypkcs11.so"}
	if err := u.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}
}

func TestPKCS11URI_ValidateConstructedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		u    *PKCS11URI
		msg  string
	}{
		{"nil", nil, "nil"},
		{"type", &PKCS11URI{Type: "wat"}, "invalid type"},
		{"version", &PKCS11URI{LibraryVersion: "999"}, "library-version"},
		{"relative module path", &PKCS11URI{ModulePath: "foo.so"}, "must be absolute"},
		{"vendor path name", &PKCS11URI{VendorPath: []VendorAttr{{Name: "bad.name", Value: "x"}}}, "invalid vendor"},
		{"vendor path shadow", &PKCS11URI{VendorPath: []VendorAttr{{Name: "TOKEN", Value: "x"}}}, "clashes"},
		{"vendor path duplicate", &PKCS11URI{VendorPath: []VendorAttr{{Name: "v", Value: "x"}, {Name: "v", Value: "y"}}}, "duplicate"},
		{"vendor query shadow", &PKCS11URI{VendorQuery: []VendorAttr{{Name: "object", Value: "x"}}}, "clashes"},
		{"invalid utf8 standard", &PKCS11URI{Token: string([]byte{0xff})}, "UTF-8"},
		{"invalid utf8 vendor", &PKCS11URI{VendorQuery: []VendorAttr{{Name: "v", Value: string([]byte{0xff})}}}, "UTF-8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.u.Validate()
			if err == nil {
				t.Fatal("Validate() succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("error %q does not contain %q", err, tt.msg)
			}
		})
	}
}

func TestPKCS11URI_PercentEncodingRules(t *testing.T) {
	t.Parallel()

	u := &PKCS11URI{
		Token:     "a/b?c#d;e&f=g",
		PinSource: "a/b?c#d;e&f=g|h",
	}
	got := u.String()
	want := "pkcs11:token=a%2Fb%3Fc%23d%3Be&f=g?pin-source=a/b?c%23d%3Be%26f=g|h"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}

	roundTrip, err := ParsePKCS11URI(got)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Token != u.Token || roundTrip.PinSource != u.PinSource {
		t.Fatalf("round trip = token %q pin-source %q", roundTrip.Token, roundTrip.PinSource)
	}
}

func TestPKCS11URI_IDRoundTripsEveryByte(t *testing.T) {
	t.Parallel()

	id := make([]byte, 256)
	for i := range id {
		id[i] = byte(i)
	}
	u := &PKCS11URI{ID: id}
	encoded := strings.TrimPrefix(u.String(), "pkcs11:id=")
	if len(encoded) != 3*len(id) {
		t.Fatalf("encoded ID length = %d, want %d", len(encoded), 3*len(id))
	}

	parsed, err := ParsePKCS11URI(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parsed.ID, id) {
		t.Fatal("ID changed after round trip")
	}
}

func TestPKCS11URI_TextMarshaling(t *testing.T) {
	t.Parallel()

	u := &PKCS11URI{Token: "My token", Object: "key", Type: ObjectTypePrivate}
	text, err := u.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(text), "pkcs11:token=My%20token;object=key;type=private"; got != want {
		t.Fatalf("MarshalText() = %q, want %q", got, want)
	}

	var decoded PKCS11URI
	if err := decoded.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	if got := decoded.String(); got != string(text) {
		t.Fatalf("UnmarshalText round trip = %q, want %q", got, text)
	}

	invalid := &PKCS11URI{Type: "bad"}
	if _, err := invalid.MarshalText(); err == nil {
		t.Fatal("MarshalText() on invalid URI succeeded")
	}
}

func TestMustParsePKCS11URI(t *testing.T) {
	t.Parallel()

	if got := MustParsePKCS11URI("pkcs11:object=k").Object; got != "k" {
		t.Fatalf("Object = %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustParsePKCS11URI did not panic")
		}
	}()
	MustParsePKCS11URI("not-pkcs11")
}

func TestPKCS11URI_ToAttributes(t *testing.T) {
	t.Parallel()

	u := &PKCS11URI{Object: "signing-key", Type: ObjectTypePrivate, ID: []byte{1, 2, 3}}
	attrs := u.ToAttributes()
	if len(attrs) != 3 {
		t.Fatalf("len(ToAttributes()) = %d, want 3", len(attrs))
	}
	if got := findAttribute(attrs, pkcs11.CKA_LABEL); got == nil || string(got.Value) != "signing-key" {
		t.Fatalf("CKA_LABEL = %#v", got)
	}
	if got := findAttribute(attrs, pkcs11.CKA_CLASS); got == nil {
		t.Fatal("CKA_CLASS missing")
	}
	if got := findAttribute(attrs, pkcs11.CKA_ID); got == nil || !bytes.Equal(got.Value, []byte{1, 2, 3}) {
		t.Fatalf("CKA_ID = %#v", got)
	}
}

func TestPKCS11URI_ToAttributesIncludesParsedEmptyValues(t *testing.T) {
	t.Parallel()

	u, err := ParsePKCS11URI("pkcs11:object=;id=")
	if err != nil {
		t.Fatal(err)
	}
	attrs := u.ToAttributes()
	if len(attrs) != 2 {
		t.Fatalf("len(ToAttributes()) = %d, want 2", len(attrs))
	}
	if got := findAttribute(attrs, pkcs11.CKA_LABEL); got == nil || len(got.Value) != 0 {
		t.Fatalf("empty CKA_LABEL = %#v", got)
	}
	if got := findAttribute(attrs, pkcs11.CKA_ID); got == nil || len(got.Value) != 0 {
		t.Fatalf("empty CKA_ID = %#v", got)
	}
}

func TestPKCS11URI_FromAttributes(t *testing.T) {
	t.Parallel()

	for _, width := range []int{4, 8} {
		t.Run(string(rune('0'+width)), func(t *testing.T) {
			classBytes := make([]byte, width)
			if width == 4 {
				binary.NativeEndian.PutUint32(classBytes, uint32(pkcs11.CKO_PRIVATE_KEY))
			} else {
				binary.NativeEndian.PutUint64(classBytes, uint64(pkcs11.CKO_PRIVATE_KEY))
			}

			u := &PKCS11URI{Token: "unchanged"}
			err := u.FromAttributes([]*pkcs11.Attribute{
				nil,
				{Type: pkcs11.CKA_LABEL, Value: []byte("key")},
				{Type: pkcs11.CKA_ID, Value: []byte{0xaa, 0xbb}},
				{Type: pkcs11.CKA_CLASS, Value: classBytes},
				{Type: 0xffffffff, Value: []byte("ignored")},
			})
			if err != nil {
				t.Fatal(err)
			}
			if u.Token != "unchanged" || u.Object != "key" || u.Type != ObjectTypePrivate || !bytes.Equal(u.ID, []byte{0xaa, 0xbb}) {
				t.Fatalf("FromAttributes() = %+v", u)
			}
		})
	}
}

func TestPKCS11URI_FromAttributesCopiesID(t *testing.T) {
	t.Parallel()

	id := []byte{1, 2, 3}
	u := &PKCS11URI{}
	if err := u.FromAttributes([]*pkcs11.Attribute{{Type: pkcs11.CKA_ID, Value: id}}); err != nil {
		t.Fatal(err)
	}
	id[0] = 9
	if u.ID[0] != 1 {
		t.Fatal("FromAttributes retained caller's ID buffer")
	}
}

func TestPKCS11URI_FromAttributesErrors(t *testing.T) {
	t.Parallel()

	badLen := &PKCS11URI{}
	if err := badLen.FromAttributes([]*pkcs11.Attribute{{Type: pkcs11.CKA_CLASS, Value: []byte{1, 2}}}); err == nil {
		t.Fatal("bad CKA_CLASS length succeeded")
	}

	unknownBytes := make([]byte, 8)
	binary.NativeEndian.PutUint64(unknownBytes, 0xdeadbeef)
	unknown := &PKCS11URI{}
	if err := unknown.FromAttributes([]*pkcs11.Attribute{{Type: pkcs11.CKA_CLASS, Value: unknownBytes}}); err == nil {
		t.Fatal("unknown CKA_CLASS succeeded")
	}

	var nilURI *PKCS11URI
	if err := nilURI.FromAttributes(nil); err == nil {
		t.Fatal("nil receiver succeeded")
	}
}

func TestPKCS11URI_IDAttr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		u        *PKCS11URI
		wantType uint
		want     []byte
		wantErr  bool
	}{
		{"prefer id", &PKCS11URI{ID: []byte{1}, Object: "label"}, pkcs11.CKA_ID, []byte{1}, false},
		{"label fallback", &PKCS11URI{Object: "label"}, pkcs11.CKA_LABEL, []byte("label"), false},
		{"none", &PKCS11URI{}, 0, nil, true},
		{"nil", nil, 0, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attr, err := tt.u.IDAttr()
			if tt.wantErr {
				if err == nil {
					t.Fatal("IDAttr() succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if attr.Type != tt.wantType || !bytes.Equal(attr.Value, tt.want) {
				t.Fatalf("IDAttr() = %#v, want type %d value %x", attr, tt.wantType, tt.want)
			}
		})
	}

	empty, err := ParsePKCS11URI("pkcs11:id=")
	if err != nil {
		t.Fatal(err)
	}
	attr, err := empty.IDAttr()
	if err != nil || attr.Type != pkcs11.CKA_ID || len(attr.Value) != 0 {
		t.Fatalf("IDAttr() for parsed empty id = %#v, %v", attr, err)
	}
}

func TestObjectTypeMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typ   ObjectType
		class uint
	}{
		{ObjectTypePublic, pkcs11.CKO_PUBLIC_KEY},
		{ObjectTypePrivate, pkcs11.CKO_PRIVATE_KEY},
		{ObjectTypeCert, pkcs11.CKO_CERTIFICATE},
		{ObjectTypeSecretKey, pkcs11.CKO_SECRET_KEY},
		{ObjectTypeData, pkcs11.CKO_DATA},
	}

	for _, tt := range tests {
		class, ok := objectTypeToClass(tt.typ)
		if !ok || class != tt.class {
			t.Fatalf("objectTypeToClass(%q) = %d, %v", tt.typ, class, ok)
		}
		typ, ok := classToObjectType(uint64(tt.class))
		if !ok || typ != tt.typ {
			t.Fatalf("classToObjectType(%d) = %q, %v", tt.class, typ, ok)
		}
	}

	if _, ok := objectTypeToClass("bad"); ok {
		t.Fatal("objectTypeToClass(bad) succeeded")
	}
	if _, ok := classToObjectType(0xdeadbeef); ok {
		t.Fatal("classToObjectType(unknown) succeeded")
	}
}

func TestDecodeCKULongUsesNativeEndian(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		hex  string
		want uint64
	}{
		{nativeHex32(0x01020304), 0x01020304},
		{nativeHex64(0x0102030405060708), 0x0102030405060708},
	} {
		b, err := hex.DecodeString(tc.hex)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeCKULong(b)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("decodeCKULong(%x) = %#x, want %#x", b, got, tc.want)
		}
	}
}

func TestNilPKCS11URIString(t *testing.T) {
	t.Parallel()
	var u *PKCS11URI
	if got := u.String(); got != "pkcs11:" {
		t.Fatalf("nil String() = %q", got)
	}
	if attrs := u.ToAttributes(); attrs != nil {
		t.Fatalf("nil ToAttributes() = %#v", attrs)
	}
}

func findAttribute(attrs []*pkcs11.Attribute, typ uint) *pkcs11.Attribute {
	for _, attr := range attrs {
		if attr != nil && attr.Type == typ {
			return attr
		}
	}
	return nil
}

func nativeHex32(v uint32) string {
	b := make([]byte, 4)
	binary.NativeEndian.PutUint32(b, v)
	return hex.EncodeToString(b)
}

func nativeHex64(v uint64) string {
	b := make([]byte, 8)
	binary.NativeEndian.PutUint64(b, v)
	return hex.EncodeToString(b)
}

func FuzzPKCS11URIRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"pkcs11:",
		"pkcs11:object=key;type=private;id=%00%FF",
		"pkcs11:serial=",
		"pkcs11:token=caf%C3%A9?vendor=x&vendor=y",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		u, err := ParsePKCS11URI(input)
		if err != nil {
			return
		}
		canonical := u.String()
		u2, err := ParsePKCS11URI(canonical)
		if err != nil {
			t.Fatalf("canonical URI %q failed to parse: %v", canonical, err)
		}
		if canonical2 := u2.String(); canonical2 != canonical {
			t.Fatalf("round trip not stable: %q -> %q", canonical, canonical2)
		}
	})
}
