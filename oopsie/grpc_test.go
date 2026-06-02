package oopsie

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToGRPCStatusRoundTripPreservesOopsiePayload(t *testing.T) {
	baseErr := Code(connect.CodeFailedPrecondition).
		Public("key does not support signing").
		In("plugin.runtime").
		New(context.Background(), "key 123 does not support signing")

	transportErr := ToGRPCStatus(baseErr)
	if got := status.Code(transportErr); got != codes.Unknown {
		t.Fatalf("status code = %v, want %v", got, codes.Unknown)
	}

	rehydrated := FromGRPCStatus(transportErr)
	var normalized *Error
	if !errors.As(rehydrated, &normalized) {
		t.Fatalf("expected rehydrated error to be oopsie.Error, got %T", rehydrated)
	}
	if normalized.Code() != connect.CodeFailedPrecondition.String() {
		t.Fatalf("normalized code = %q, want %q", normalized.Code(), connect.CodeFailedPrecondition.String())
	}
	if normalized.Public() != "key does not support signing" {
		t.Fatalf("normalized public = %q", normalized.Public())
	}
	if normalized.Error() != "key 123 does not support signing" {
		t.Fatalf("normalized message = %q", normalized.Error())
	}
}

func TestToGRPCStatusRoundTripHandlesPlainErrors(t *testing.T) {
	transportErr := ToGRPCStatus(errors.New("plain failure"))
	rehydrated := FromGRPCStatus(transportErr)

	var normalized *Error
	if !errors.As(rehydrated, &normalized) {
		t.Fatalf("expected rehydrated error to be oopsie.Error, got %T", rehydrated)
	}
	if normalized.Error() != "plain failure" {
		t.Fatalf("normalized message = %q", normalized.Error())
	}
	if normalized.Code() != connect.CodeInternal.String() {
		t.Fatalf("normalized code = %q, want %q", normalized.Code(), connect.CodeInternal.String())
	}
}

func TestFromGRPCStatusFallsBackWhenNoPayloadExists(t *testing.T) {
	original := status.Error(codes.NotFound, "not found")
	rehydrated := FromGRPCStatus(original)
	if rehydrated != original {
		t.Fatalf("expected original grpc error when no payload exists")
	}
}

func TestGRPCCodeOfUsesGenericUnknownForErrors(t *testing.T) {
	if got := GRPCCodeOf(errors.New("boom")); got != codes.Unknown {
		t.Fatalf("code = %v, want %v", got, codes.Unknown)
	}
}
