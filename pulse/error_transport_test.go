package pulse

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/otpki/otpki-pkg/oopsie"
	"github.com/samber/oops"
)

func TestEnvelopeErrorFromPreservesOopsiePayload(t *testing.T) {
	err := oops.Code("failed_precondition").
		Public("cannot do that").
		In("issuance").
		With("field", "status").
		New("internal failure")

	envelopeErr := envelopeErrorFrom(err)
	if envelopeErr == nil {
		t.Fatal("expected envelope error")
	}
	if len(envelopeErr.Oopsie) == 0 {
		t.Fatal("expected serialized oopsie payload")
	}

	roundTrip := errorFromEnvelope(envelopeErr)

	var oopsieErr *oopsie.Error
	if !errors.As(roundTrip, &oopsieErr) {
		t.Fatalf("expected oopsie error, got %T", roundTrip)
	}
	if got := oopsie.ConnectCodeOf(roundTrip); got != connect.CodeFailedPrecondition {
		t.Fatalf("unexpected connect code: %v", got)
	}
	if oopsieErr.Domain() != "issuance" {
		t.Fatalf("unexpected domain: %q", oopsieErr.Domain())
	}
	if oopsieErr.Details()["context"] == nil {
		t.Fatalf("expected details to preserve context: %#v", oopsieErr.Details())
	}
}

func TestEnvelopeErrorFromUsesConnectCodeWhenPresent(t *testing.T) {
	base := oops.Public("bad request").New("invalid input")
	err := connect.NewError(connect.CodeInvalidArgument, base)

	envelopeErr := envelopeErrorFrom(err)
	if envelopeErr == nil {
		t.Fatal("expected envelope error")
	}

	roundTrip := errorFromEnvelope(envelopeErr)
	if got := oopsie.ConnectCodeOf(roundTrip); got != connect.CodeInvalidArgument {
		t.Fatalf("unexpected code: %v", got)
	}
}

func TestEnvelopeErrorFromPlainErrorFallsBack(t *testing.T) {
	envelopeErr := envelopeErrorFrom(errors.New("boom"))
	if envelopeErr == nil {
		t.Fatal("expected envelope error")
	}
	if len(envelopeErr.Oopsie) == 0 {
		t.Fatal("expected fallback oopsie payload")
	}

	roundTrip := errorFromEnvelope(envelopeErr)
	if got := oopsie.ConnectCodeOf(roundTrip); got != connect.CodeInternal {
		t.Fatalf("unexpected code: %v", got)
	}
	if roundTrip.Error() != "boom" {
		t.Fatalf("unexpected message: %q", roundTrip.Error())
	}
}

func TestEnvelopeErrorFromKeepsInternalForNonConnectOopsCode(t *testing.T) {
	err := oops.Code("database_error").New("db failed")

	envelopeErr := envelopeErrorFrom(err)
	if envelopeErr == nil {
		t.Fatal("expected envelope error")
	}
	roundTrip := errorFromEnvelope(envelopeErr)
	if got := oopsie.ConnectCodeOf(roundTrip); got != connect.CodeInternal {
		t.Fatalf("unexpected code: %v", got)
	}
}

func TestErrorFromEnvelopeRejectsMissingOopsiePayload(t *testing.T) {
	err := errorFromEnvelope(&EnvelopeError{})
	if got := oopsie.ConnectCodeOf(err); got != connect.CodeInternal {
		t.Fatalf("unexpected connect code: %v", got)
	}
	if err.Error() != "response envelope error missing oopsie payload" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestErrorFromEnvelopeRejectsInvalidOopsiePayload(t *testing.T) {
	err := errorFromEnvelope(&EnvelopeError{
		Oopsie: []byte(`{`),
	})
	if got := oopsie.ConnectCodeOf(err); got != connect.CodeInternal {
		t.Fatalf("unexpected connect code: %v", got)
	}
	if err.Error() != "response envelope contained invalid oopsie payload" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}
