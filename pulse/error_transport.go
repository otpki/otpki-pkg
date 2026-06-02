package pulse

import (
	"encoding/json"
	"errors"

	"connectrpc.com/connect"
	"github.com/otpki/otpki-pkg/oopsie"
)

func envelopeErrorFrom(err error) *EnvelopeError {
	if err == nil {
		return nil
	}

	normalized := oopsie.FromError(err)
	envelope := normalized.Envelope()

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		envelope.Code = connectErr.Code().String()
	}

	payload, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		payload = mustMarshalProtocolFallback(err, marshalErr)
	}

	return &EnvelopeError{
		Oopsie: payload,
	}
}

func errorFromEnvelope(envelopeErr *EnvelopeError) error {
	if envelopeErr == nil {
		return nil
	}

	if len(envelopeErr.Oopsie) == 0 {
		return invalidErrorEnvelopePayloadError("response envelope error missing oopsie payload")
	}

	normalized, err := oopsie.Unmarshal(envelopeErr.Oopsie)
	if err != nil {
		return invalidErrorEnvelopePayloadError("response envelope contained invalid oopsie payload")
	}
	if normalized == nil {
		return invalidErrorEnvelopePayloadError("response envelope error contained empty oopsie payload")
	}

	return normalized
}

func missingResponsePayloadError() error {
	return oopsie.In(ServiceName).Code(connect.CodeInternal).Base().New("response envelope contained neither payload nor error")
}

func invalidErrorEnvelopePayloadError(message string) error {
	return oopsie.In(ServiceName).Code(connect.CodeInternal).Base().New(message)
}

func mustMarshalProtocolFallback(err error, marshalErr error) []byte {
	payload, fallbackErr := json.Marshal(oopsie.Envelope{
		Version: oopsie.EnvelopeVersion,
		Raw:     true,
		Message: err.Error(),
		Public:  err.Error(),
		Code:    connect.CodeInternal.String(),
		Details: map[string]any{
			"error":         err.Error(),
			"public":        err.Error(),
			"code":          connect.CodeInternal.String(),
			"marshal_error": marshalErr.Error(),
		},
	})
	if fallbackErr != nil {
		payload = []byte(`{"version":1,"raw":true,"message":"internal","public":"internal","code":"internal"}`)
	}
	return payload
}
