package nonce_test

import (
	"testing"

	"github.com/otpki/otpki-pkg/nonce"
)

func TestNonceManager(t *testing.T) {
	manager := nonce.NewLocalMemoryNonceManager()
	n1, _ := manager.CreateNonce(t.Context())
	n2, _ := manager.CreateNonce(t.Context())
	if n1 == n2 {
		t.Fatalf("nonces should not be equal %s == %s", n1, n2)
	}
	if ok, err := manager.CheckNonce(t.Context(), n1); !ok || err != nil {
		t.Fatal("should be valid nonce")
	}
	if ok, err := manager.CheckNonce(t.Context(), n1); ok || err != nil {
		t.Fatal("should no longer be valid nonce")
	}
	if ok, err := manager.CheckNonce(t.Context(), n2); !ok || err != nil {
		t.Fatal("should be valid nonce")
	}
	if ok, err := manager.CheckNonce(t.Context(), n2); ok || err != nil {
		t.Fatal("should no longer be valid nonce")
	}
}
