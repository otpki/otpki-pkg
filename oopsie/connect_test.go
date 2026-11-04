package oopsie

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/stretchr/testify/require"
)

func TestToPublicConnectErrorUsesPublicMessageAndCode(t *testing.T) {
	err := Code(connect.CodeFailedPrecondition).
		Public("public failure").
		In("test").
		From(context.Background()).
		New("internal failure")

	got := ToPublicConnectError(err)

	connectErr, ok := errors.AsType[*connect.Error](got)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", got)
	}
	if connectErr.Code() != connect.CodeFailedPrecondition {
		t.Fatalf("code = %v, want %v", connectErr.Code(), connect.CodeFailedPrecondition)
	}
	if connectErr.Message() != "public failure" {
		t.Fatalf("message = %q, want %q", connectErr.Message(), "public failure")
	}
}

func TestToPublicConnectErrorFallsBackToInternalMessage(t *testing.T) {
	err := In("test").From(context.Background()).New("real internal message")

	got := ToPublicConnectError(err)

	connectErr, ok := errors.AsType[*connect.Error](got)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", got)
	}
	if connectErr.Code() != connect.CodeInternal {
		t.Fatalf("code = %v, want %v", connectErr.Code(), connect.CodeInternal)
	}
	if connectErr.Message() != "real internal message" {
		t.Fatalf("message = %q, want %q", connectErr.Message(), "real internal message")
	}
}

func TestToPublicConnectErrorBestEffortPlainError(t *testing.T) {
	got := ToPublicConnectError(errors.New("plain failure"))

	connectErr, ok := errors.AsType[*connect.Error](got)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T", got)
	}
	if connectErr.Code() != connect.CodeInternal {
		t.Fatalf("code = %v, want %v", connectErr.Code(), connect.CodeInternal)
	}
	if connectErr.Message() != "plain failure" {
		t.Fatalf("message = %q, want %q", connectErr.Message(), "plain failure")
	}
}

func TestConnectCodeOfUnderstandsOopsieErrors(t *testing.T) {
	err := Code(connect.CodeUnauthenticated).
		Public("authentication required").
		From(context.Background()).
		New("unauthenticated")

	if got := ConnectCodeOf(err); got != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want %v", got, connect.CodeUnauthenticated)
	}
}

func TestToPublicConnectErrorRetainsCause(t *testing.T) {
	cause := errors.New("internal failure")
	original := Code(connect.CodePermissionDenied).Public("access denied").Wrap(context.Background(), cause)
	converted := ToPublicConnectError(original)
	if !errors.Is(converted, cause) {
		t.Fatal("lost server-side cause")
	}
	connectErr, ok := errors.AsType[*connect.Error](converted)
	if !ok {
		t.Fatalf("expected Connect error, got %T", converted)
	}
	if connectErr.Message() != "access denied" {
		t.Fatal(converted)
	}
	require.Same(t, converted, ToPublicConnectError(converted), "existing Connect error should be preserved")
}

func TestOopsConnectCodeRejectsIntegerOverflow(t *testing.T) {
	for _, value := range []any{int(-1), int32(-1), int64(-1), int64(1 << 32), int64(1<<32) + int64(connect.CodePermissionDenied)} {
		_, ok := oopsConnectCode(value)
		if ok {
			t.Errorf("accepted out-of-range code %v", value)
		}
	}
	for _, value := range []any{int(connect.CodePermissionDenied), int32(connect.CodePermissionDenied), int64(connect.CodePermissionDenied)} {
		code, ok := oopsConnectCode(value)
		if !ok || code != connect.CodePermissionDenied {
			t.Errorf("lost valid code %v: %v, %v", value, code, ok)
		}
	}
}
