package oopsie

import (
	"errors"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/samber/oops"
)

func ToPublicConnectError(err error) error {
	if err == nil {
		return nil
	}

	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
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

	return connect.NewError(code, public).WithCause(err)
}

func ConnectCodeOf(err error) connect.Code {
	if err == nil {
		return 0
	}

	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
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
		return numericConnectCode(int64(v))
	case int32:
		return numericConnectCode(int64(v))
	case int64:
		return numericConnectCode(v)
	case string:
		return ParseConnectCode(v)
	default:
		return 0, false
	}
}

func numericConnectCode(v int64) (connect.Code, bool) {
	if v < 0 || v > 0xffffffff {
		return 0, false
	}
	return connect.Code(v), true
}
