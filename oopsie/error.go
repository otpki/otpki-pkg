package oopsie

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samber/oops"
)

// EnvelopeVersion is the current internal envelope format version.
const EnvelopeVersion = 1

// Envelope is the portable internal representation for an application error.
// It preserves oops metadata when available and falls back to a best-effort
// shape for plain errors.
type Envelope struct {
	Version int            `json:"version"`
	Raw     bool           `json:"raw,omitempty"`
	Message string         `json:"message"`
	Public  string         `json:"public,omitempty"`
	Code    string         `json:"code,omitempty"`
	Domain  string         `json:"domain,omitempty"`
	Trace   string         `json:"trace,omitempty"`
	Span    string         `json:"span,omitempty"`
	Hint    string         `json:"hint,omitempty"`
	Owner   string         `json:"owner,omitempty"`
	Tags    []string       `json:"tags,omitempty"`
	Context map[string]any `json:"context,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// Error is the normalized error type produced by envelope deserialization or
// best-effort conversion from a generic error.
type Error struct {
	envelope Envelope
	cause    error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.envelope.Message != "" {
		return e.envelope.Message
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return ""
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *Error) MarshalJSON() ([]byte, error) {
	if e == nil {
		return json.Marshal(nil)
	}
	return json.Marshal(e.envelope)
}

func (e *Error) Envelope() Envelope {
	if e == nil {
		return Envelope{}
	}
	return e.envelope
}

func (e *Error) Code() string {
	if e == nil {
		return ""
	}
	return e.envelope.Code
}

func (e *Error) Public() string {
	if e == nil {
		return ""
	}
	return e.envelope.Public
}

func (e *Error) Domain() string {
	if e == nil {
		return ""
	}
	return e.envelope.Domain
}

func (e *Error) Trace() string {
	if e == nil {
		return ""
	}
	return e.envelope.Trace
}

func (e *Error) Span() string {
	if e == nil {
		return ""
	}
	return e.envelope.Span
}

func (e *Error) ToMap() map[string]any {
	if e == nil {
		return nil
	}
	return envelopeToMap(e.envelope)
}

func (e *Error) Details() map[string]any {
	if e == nil {
		return nil
	}
	return e.envelope.Details
}

func ToMap(err error) map[string]any {
	if err == nil {
		return nil
	}
	return FromError(err).ToMap()
}

// AsError returns an existing normalized error if one is already present in the
// error chain.
func AsError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}

	var oopsieErr *Error
	if errors.As(err, &oopsieErr) {
		return oopsieErr, true
	}

	return nil, false
}

// FromError normalizes any error into an oopsie Error. If the error already
// contains rich oops metadata, it is preserved. Otherwise a best-effort raw
// envelope is created without inventing new stack or trace data.
func FromError(err error) *Error {
	if err == nil {
		return nil
	}

	if normalized, ok := AsError(err); ok {
		return normalized
	}

	if oopsErr, ok := oops.AsOops(err); ok {
		return &Error{
			envelope: envelopeFromOops(oopsErr),
			cause:    err,
		}
	}

	return &Error{
		envelope: Envelope{
			Version: EnvelopeVersion,
			Raw:     true,
			Message: err.Error(),
			Public:  err.Error(),
			Code:    "internal",
			Details: map[string]any{
				"error":  err.Error(),
				"public": err.Error(),
				"code":   "internal",
			},
		},
		cause: err,
	}
}

// Marshal converts any error into the internal envelope JSON form.
func Marshal(err error) ([]byte, error) {
	if err == nil {
		return json.Marshal(nil)
	}
	return json.Marshal(FromError(err).envelope)
}

// Unmarshal decodes an internal envelope JSON payload back into a normalized
// error.
func Unmarshal(data []byte) (*Error, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, oops.Errorf("unmarshal oopsie envelope: %w", err)
	}

	if envelope.Version == 0 {
		envelope.Version = EnvelopeVersion
	}

	return &Error{envelope: envelope}, nil
}

func envelopeFromOops(err oops.OopsError) Envelope {
	return Envelope{
		Version: EnvelopeVersion,
		Message: err.Error(),
		Public:  err.Public(),
		Code:    fmt.Sprint(err.Code()),
		Domain:  err.Domain(),
		Trace:   err.Trace(),
		Span:    err.Span(),
		Hint:    err.Hint(),
		Owner:   err.Owner(),
		Tags:    err.Tags(),
		Context: err.Context(),
		Details: err.ToMap(),
	}
}

func envelopeToMap(envelope Envelope) map[string]any {
	data, err := json.Marshal(envelope)
	if err != nil {
		return map[string]any{
			"version": envelope.Version,
			"message": envelope.Message,
		}
	}

	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{
			"version": envelope.Version,
			"message": envelope.Message,
		}
	}

	return out
}
