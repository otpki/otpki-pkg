package dnssec

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

// IANARootTrustAnchors returns the current ICANN root KSK trust anchors. A new
// slice is returned so callers cannot mutate package defaults.
func IANARootTrustAnchors() []TrustAnchor {
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

// AddTrustAnchor adds one permanent trust anchor to the validator.
func (v *Validator) AddTrustAnchor(anchor TrustAnchor) {
	v.SetAnchor(anchor.Owner, anchor.KeyTag, anchor.Algorithm, anchor.DigestType, anchor.Digest)
}
