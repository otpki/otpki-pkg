package dnssec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIANARootTrustAnchorsReturnsCopy(t *testing.T) {
	t.Parallel()

	first := IANARootTrustAnchors()
	require.Len(t, first, 2)
	first[0].Digest = "changed"

	second := IANARootTrustAnchors()
	require.NotEqual(t, first[0].Digest, second[0].Digest)
	require.Equal(t, uint16(20326), second[0].KeyTag)
	require.Equal(t, uint16(38696), second[1].KeyTag)
}
