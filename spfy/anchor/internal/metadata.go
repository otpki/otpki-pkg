package internal

import (
	"slices"

	"github.com/otpki/otpki-pkg/spfy/anchor"
)

func CloneMetadata(m anchor.Metadata) anchor.Metadata {
	if len(m) == 0 {
		return nil
	}
	out := make(anchor.Metadata, len(m))
	for k, values := range m {
		out[k] = slices.Clone(values)
	}
	return out
}

func MergeMetadata(base anchor.Metadata, extra anchor.Metadata) anchor.Metadata {
	out := CloneMetadata(base)
	if out == nil && len(extra) > 0 {
		out = anchor.Metadata{}
	}
	for k, values := range extra {
		out[k] = append(out[k], values...)
	}
	return out
}

func MetadataContains(have anchor.Metadata, want anchor.Metadata) bool {
	for k, values := range want {
		haveValues, ok := have[k]
		if !ok {
			return false
		}
		for _, wantValue := range values {
			found := false
			for _, haveValue := range haveValues {
				if haveValue == wantValue {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
