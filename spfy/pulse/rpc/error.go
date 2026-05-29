package rpc

import (
	"errors"

	"connectrpc.com/connect"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/oops"
)

const (
	HeaderErrorCode    = "pulse-rpc-error-code"
	HeaderErrorMessage = "pulse-rpc-error-message"
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
	h := make(pulse.Header, 2)
	h.Set(HeaderErrorCode, code.String())
	h.Set(HeaderErrorMessage, msg)
	return pulse.Message{Header: h}
}

func ErrorFromMessage(msg pulse.Message) error {
	if len(msg.Header) == 0 {
		return nil
	}
	code := msg.Header.Get(HeaderErrorCode)
	if code == "" {
		return nil
	}
	connectCode := connect.CodeUnknown
	_ = connectCode.UnmarshalText([]byte(code)) // ignore error, just use code unknown
	return connect.NewError(connectCode, oops.New(msg.Header.Get(HeaderErrorMessage)))
}
