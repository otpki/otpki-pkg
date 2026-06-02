package oopsie

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
)

func TestFromErrorPlainErrorFallsBack(t *testing.T) {
	err := errors.New("plain failure")

	got := FromError(err)
	if got == nil {
		t.Fatal("expected normalized error")
	}

	if got.Error() != "plain failure" {
		t.Fatalf("unexpected message: %q", got.Error())
	}
	if !got.envelope.Raw {
		t.Fatal("expected raw fallback envelope")
	}
	if got.envelope.Code != "internal" {
		t.Fatalf("unexpected code: %q", got.envelope.Code)
	}
	if got.Details()["error"] != "plain failure" {
		t.Fatalf("unexpected details payload: %#v", got.Details())
	}
}

func TestFromErrorOopsPreservesMetadata(t *testing.T) {
	err := oops.Code("invalid_argument").
		Public("bad request").
		In("test-domain").
		Trace("trace-123").
		Span("span-456").
		With("field", "name").
		New("internal failure")

	got := FromError(err)
	if got == nil {
		t.Fatal("expected normalized error")
	}

	if got.Public() != "bad request" {
		t.Fatalf("unexpected public message: %q", got.Public())
	}
	if got.Code() != "invalid_argument" {
		t.Fatalf("unexpected code: %q", got.Code())
	}
	if got.Domain() != "test-domain" {
		t.Fatalf("unexpected domain: %q", got.Domain())
	}
	if got.Trace() != "trace-123" {
		t.Fatalf("unexpected trace: %q", got.Trace())
	}
	if got.Span() != "span-456" {
		t.Fatalf("unexpected span: %q", got.Span())
	}
	if got.envelope.Context["field"] != "name" {
		t.Fatalf("unexpected context value: %#v", got.envelope.Context)
	}
	if got.Details()["error"] != "internal failure" {
		t.Fatalf("unexpected details payload: %#v", got.Details())
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	err := oops.Code("not_found").Public("missing").In("domain").New("internal missing")

	data, marshalErr := Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal failed: %v", marshalErr)
	}

	got, unmarshalErr := Unmarshal(data)
	if unmarshalErr != nil {
		t.Fatalf("unmarshal failed: %v", unmarshalErr)
	}

	if got == nil {
		t.Fatal("expected round-tripped error")
	}
	if got.Code() != "not_found" {
		t.Fatalf("unexpected code: %q", got.Code())
	}
	if got.Public() != "missing" {
		t.Fatalf("unexpected public message: %q", got.Public())
	}
	if got.Domain() != "domain" {
		t.Fatalf("unexpected domain: %q", got.Domain())
	}
}

func TestFromErrorPreservesExistingOopsieError(t *testing.T) {
	normalized := FromError(errors.New("plain failure"))
	got := FromError(normalized)

	if got != normalized {
		t.Fatal("expected existing normalized error to be reused")
	}
}

func TestWrapUsesRegisteredContextExtractor(t *testing.T) {
	resetContextExtractorsForTesting()
	RegisterContextExtractor(func(ctx context.Context, builder oops.OopsErrorBuilder) oops.OopsErrorBuilder {
		if traceID, ok := ctx.Value(traceKey{}).(string); ok {
			builder = builder.Trace(traceID)
		}
		return builder
	})

	err := Wrap(context.WithValue(context.Background(), traceKey{}, "trace-ctx"), errors.New("boom"))
	got := FromError(err)

	if got.Trace() != "trace-ctx" {
		t.Fatalf("unexpected trace: %q", got.Trace())
	}
}

func TestWrapUsesOopsBuilderFromContext(t *testing.T) {
	resetContextExtractorsForTesting()
	ctx := WithBuilder(context.Background(), Code("permission_denied").Public("denied").In("ctx-domain"))

	err := Wrap(ctx, errors.New("boom"))
	got := FromError(err)

	if got.Code() != "permission_denied" {
		t.Fatalf("unexpected code: %q", got.Code())
	}
	if got.Public() != "denied" {
		t.Fatalf("unexpected public: %q", got.Public())
	}
	if got.Domain() != "ctx-domain" {
		t.Fatalf("unexpected domain: %q", got.Domain())
	}
}

func TestBuilderWrapPreservesExplicitMetadata(t *testing.T) {
	resetContextExtractorsForTesting()
	err := Code("failed_precondition").
		Public("cannot do that").
		In("test-domain").
		Wrap(context.Background(), errors.New("boom"))

	got := FromError(err)

	if got.Code() != "failed_precondition" {
		t.Fatalf("unexpected code: %q", got.Code())
	}
	if got.Public() != "cannot do that" {
		t.Fatalf("unexpected public: %q", got.Public())
	}
	if got.Domain() != "test-domain" {
		t.Fatalf("unexpected domain: %q", got.Domain())
	}
}

func TestFromWithoutContextDoesNotEnrich(t *testing.T) {
	resetContextExtractorsForTesting()
	RegisterContextExtractor(func(ctx context.Context, builder oops.OopsErrorBuilder) oops.OopsErrorBuilder {
		if traceID, ok := ctx.Value(traceKey{}).(string); ok {
			builder = builder.Trace(traceID)
		}
		return builder
	})

	err := Base().Code("failed_precondition").New("boom")
	got := FromError(err)

	if got.Trace() == "trace-ctx" {
		t.Fatalf("unexpected extractor trace: %q", got.Trace())
	}
}

func TestBuilderWithContextOptInEnriches(t *testing.T) {
	resetContextExtractorsForTesting()
	RegisterContextExtractor(func(ctx context.Context, builder oops.OopsErrorBuilder) oops.OopsErrorBuilder {
		if traceID, ok := ctx.Value(traceKey{}).(string); ok {
			builder = builder.Trace(traceID)
		}
		return builder
	})

	ctx := context.WithValue(context.Background(), traceKey{}, "trace-ctx")
	err := Code("failed_precondition").From(ctx).New("boom")
	got := FromError(err)

	if got.Trace() != "trace-ctx" {
		t.Fatalf("unexpected trace: %q", got.Trace())
	}
}

type traceKey struct{}
