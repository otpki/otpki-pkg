package pulse

import (
	"github.com/samber/oops"
)

var (
	ErrClosed      = oops.New("pulse: closed")
	ErrInvalidName = oops.New("pulse: invalid subject or group")
	ErrNoHandler   = oops.New("pulse: no handler")
	ErrNoResult    = oops.New("pulse: result channel closed without result")
	ErrTimeout     = oops.New("pulse: timeout")
	ErrUnsupported = oops.New("pulse: unsupported operation")
)
