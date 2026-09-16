package dnssec

import "sync"

// TrustAnchor identifies a DNSSEC DS record trusted without walking to its
// parent. The owner is normally "." for the public DNS root, but may name an
// internal enterprise zone.
type TrustAnchor struct {
	Owner      string
	KeyTag     uint16
	Algorithm  uint8
	DigestType uint8
	Digest     string
}

var ianaRootTrustAnchorState = struct {
	sync.RWMutex
	anchors []TrustAnchor
}{
	anchors: embeddedIANARootTrustAnchors(),
}

// IANARootTrustAnchors returns the currently active ICANN root KSK trust
// anchors. The set initially contains embedded known-good anchors and may be
// replaced by RefreshIANARootTrustAnchors after verifying IANA's signed
// publication. A new slice is returned so callers cannot mutate package state.
func IANARootTrustAnchors() []TrustAnchor {
	ianaRootTrustAnchorState.RLock()
	defer ianaRootTrustAnchorState.RUnlock()
	return append([]TrustAnchor(nil), ianaRootTrustAnchorState.anchors...)
}

func embeddedIANARootTrustAnchors() []TrustAnchor {
	return []TrustAnchor{
		{
			Owner:      ".",
			KeyTag:     20326,
			Algorithm:  8,
			DigestType: 2,
			Digest:     "E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D",
		},
		{
			Owner:      ".",
			KeyTag:     38696,
			Algorithm:  8,
			DigestType: 2,
			Digest:     "683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16",
		},
	}
}

func replaceIANARootTrustAnchors(anchors []TrustAnchor) {
	ianaRootTrustAnchorState.Lock()
	defer ianaRootTrustAnchorState.Unlock()
	ianaRootTrustAnchorState.anchors = append([]TrustAnchor(nil), anchors...)
}

// AddTrustAnchor adds one permanent trust anchor to the validator.
func (v *Validator) AddTrustAnchor(anchor TrustAnchor) {
	v.SetAnchor(anchor.Owner, anchor.KeyTag, anchor.Algorithm, anchor.DigestType, anchor.Digest)
}
