package rpc

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/otpki/otpki-pkg/oopsie"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/oops"
)

func TestErrorMessageRoundTripPreservesOopsiePayload(t *testing.T) {
	err := oops.Code("failed_precondition").
		Public("cannot do that").
		In("issuance").
		With("field", "status").
		New("internal failure")

	roundTrip := ErrorFromMessage(ErrorMessage(err))

	var oopsieErr *oopsie.Error
	if !errors.As(roundTrip, &oopsieErr) {
		t.Fatalf("expected oopsie error, got %T", roundTrip)
	}
	if got := oopsie.ConnectCodeOf(roundTrip); got != connect.CodeFailedPrecondition {
		t.Fatalf("unexpected connect code: %v", got)
	}
	if got := oopsieErr.Domain(); got != "issuance" {
		t.Fatalf("unexpected domain: %q", got)
	}
	if oopsieErr.Details()["context"] == nil {
		t.Fatalf("expected context details to survive round trip: %#v", oopsieErr.Details())
	}
}

func TestErrorMessageRoundTripPreservesConnectCodeOverride(t *testing.T) {
	base := oops.Public("bad request").New("invalid input")
	err := connect.NewError(connect.CodeInvalidArgument, base)

	roundTrip := ErrorFromMessage(ErrorMessage(err))

	if got := oopsie.ConnectCodeOf(roundTrip); got != connect.CodeInvalidArgument {
		t.Fatalf("unexpected code: %v", got)
	}
}

func TestErrorFromMessageLegacyHeadersStillWork(t *testing.T) {
	msg := pulse.Message{
		Header: pulse.Header{
			HeaderErrorCode:    {connect.CodeFailedPrecondition.String()},
			HeaderErrorMessage: {"legacy failure"},
		},
	}

	err := ErrorFromMessage(msg)

	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Fatalf("unexpected code: %v", got)
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Message() != "legacy failure" {
		t.Fatalf("unexpected message: %q", connectErr.Message())
	}
}

func TestErrorFromMessageFallsBackWhenStructuredPayloadInvalid(t *testing.T) {
	msg := pulse.Message{
		Header: pulse.Header{
			HeaderErrorOopsie:  {"%%%"},
			HeaderErrorCode:    {connect.CodeNotFound.String()},
			HeaderErrorMessage: {"legacy missing"},
		},
	}

	err := ErrorFromMessage(msg)

	if got := connect.CodeOf(err); got != connect.CodeNotFound {
		t.Fatalf("unexpected code: %v", got)
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %T", err)
	}
	if connectErr.Message() != "legacy missing" {
		t.Fatalf("unexpected message: %q", connectErr.Message())
	}
}
