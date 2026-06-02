package oopsie

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
)

func TestToPublicConnectErrorUsesPublicMessageAndCode(t *testing.T) {
	err := Code(connect.CodeFailedPrecondition).
		Public("public failure").
		In("test").
		From(context.Background()).
		New("internal failure")

	got := ToPublicConnectError(err)

	connectErr, ok := got.(*connect.Error)
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

	connectErr, ok := got.(*connect.Error)
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

	connectErr, ok := got.(*connect.Error)
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
