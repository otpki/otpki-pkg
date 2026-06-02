package oopsie

import (
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/samber/oops"
)

type publicConnectError struct {
	public string
	cause  error
}

func (e publicConnectError) Error() string {
	return e.public
}

func (e publicConnectError) Unwrap() error {
	return e.cause
}

func (e publicConnectError) Format(state fmt.State, verb rune) {
	switch verb {
	case 'v':
		if state.Flag('+') && e.cause != nil {
			_, _ = fmt.Fprintf(state, "%s: %+v", e.public, e.cause)
			return
		}
		fallthrough
	case 's':
		_, _ = fmt.Fprint(state, e.public)
	case 'q':
		_, _ = fmt.Fprintf(state, "%q", e.public)
	}
}

func ToPublicConnectError(err error) error {
	if err == nil {
		return nil
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}

	normalized := FromError(err)
	code, ok := ParseConnectCode(normalized.Code())
	if !ok {
		code = connect.CodeInternal
	}

	public := strings.TrimSpace(normalized.Public())
	if public == "" {
		public = strings.TrimSpace(normalized.Error())
	}
	if public == "" {
		public = DefaultPublicMessage(code)
	}

	return connect.NewError(code, publicConnectError{
		public: public,
		cause:  err,
	})
}

func ConnectCodeOf(err error) connect.Code {
	if err == nil {
		return 0
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code()
	}

	if normalized, ok := AsError(err); ok {
		if code, ok := ParseConnectCode(normalized.Code()); ok {
			return code
		}
	}

	if oopsErr, ok := oops.AsOops(err); ok {
		if code, ok := oopsConnectCode(oopsErr.Code()); ok {
			return code
		}
	}

	return connect.CodeInternal
}

func ParseConnectCode(raw string) (connect.Code, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")

	var code connect.Code
	if err := code.UnmarshalText([]byte(normalized)); err != nil {
		return 0, false
	}
	return code, true
}

func DefaultPublicMessage(code connect.Code) string {
	return code.String()
}

func oopsConnectCode(code any) (connect.Code, bool) {
	switch v := code.(type) {
	case connect.Code:
		return v, true
	case int:
		return connect.Code(v), true
	case int32:
		return connect.Code(v), true
	case int64:
		return connect.Code(v), true
	case string:
		return ParseConnectCode(v)
	default:
		return 0, false
	}
}
