package rpc

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"connectrpc.com/connect"
	"github.com/otpki/otpki-pkg/oopsie"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/oops"
)

const (
	HeaderErrorCode    = "pulse-rpc-error-code"
	HeaderErrorMessage = "pulse-rpc-error-message"
	HeaderErrorOopsie  = "pulse-rpc-error-oopsie-bin"
)

func ErrorMessage(err error) pulse.Message {
	if err == nil {
		return pulse.Message{}
	}
	code := connect.CodeOf(err)
	msg := err.Error()
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		msg = connectErr.Message()
	}
	h := make(pulse.Header, 3)
	h.Set(HeaderErrorCode, code.String())
	h.Set(HeaderErrorMessage, msg)
	if payload, marshalErr := marshalOopsiePayload(err); marshalErr == nil && len(payload) > 0 {
		h.Set(HeaderErrorOopsie, base64.RawStdEncoding.EncodeToString(payload))
	}
	return pulse.Message{Header: h}
}

func ErrorFromMessage(msg pulse.Message) error {
	if len(msg.Header) == 0 {
		return nil
	}
	if payload := msg.Header.Get(HeaderErrorOopsie); payload != "" {
		if raw, err := base64.RawStdEncoding.DecodeString(payload); err == nil {
			if normalized, err := oopsie.Unmarshal(raw); err == nil && normalized != nil {
				return normalized
			}
		}
	}
	code := msg.Header.Get(HeaderErrorCode)
	if code == "" {
		return nil
	}
	connectCode := connect.CodeUnknown
	_ = connectCode.UnmarshalText([]byte(code)) // ignore error, just use code unknown
	return connect.NewError(connectCode, oops.New(msg.Header.Get(HeaderErrorMessage)))
}

func marshalOopsiePayload(err error) ([]byte, error) {
	normalized := oopsie.FromError(err)
	if normalized == nil {
		return json.Marshal(nil)
	}

	envelope := normalized.Envelope()
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		envelope.Code = connectErr.Code().String()
	}

	return json.Marshal(envelope)
}
