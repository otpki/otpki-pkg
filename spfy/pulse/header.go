package pulse

import (
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
)

// EncodedHeaderKey is the broker metadata key used by backends that only support
// string metadata. Applications should avoid using this key directly.
const EncodedHeaderKey = "pulse-header-bin"

// Header is the metadata carried with every Pulse message. It intentionally
// matches http.Header's multi-value shape so Connect metadata round-trips
// without lossy conversions.
type Header map[string][]string

func (h Header) Clone() Header {
	if len(h) == 0 {
		return nil
	}

	out := maps.Clone(h)
	for k, vs := range out {
		out[k] = slices.Clone(vs)
	}
	return out
}

func (h Header) Get(key string) string {
	if len(h[key]) == 0 {
		return ""
	}
	return h[key][0]
}

func (h Header) Values(key string) []string {
	return slices.Clone(h[key])
}

func (h Header) Set(key, value string) {
	if h != nil {
		h[key] = []string{value}
	}
}

func (h Header) Add(key, value string) {
	if h != nil {
		h[key] = append(h[key], value)
	}
}

func HeaderFromHTTP(src http.Header) Header {
	return Header(src).Clone()
}

func CopyHeaderToHTTP(src Header, dst http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func EncodeHeader(h Header) (string, error) {
	if len(h) == 0 {
		return "", nil
	}

	b, err := json.Marshal(h)
	if err != nil {
		return "", err
	}

	return base64.RawStdEncoding.EncodeToString(b), nil
}

func DecodeHeader(s string) (Header, error) {
	if s == "" {
		return nil, nil
	}

	b, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}

	var h Header
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, err
	}

	return h, nil
}
