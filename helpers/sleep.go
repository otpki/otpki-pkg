package helpers

import (
	"context"
	"time"
)

// Abortable sleep with channel
// Returns if slept for full duration, false if interrupted
func SleepWithWakeChannel(wake <-chan bool, d time.Duration) bool {
	timer := time.NewTimer(d)
	select {
	case <-wake:
		timer.Stop()
		return false
	case <-timer.C:
		return true
	}
}

// Abortable sleep with cancellable context
// Returns if slept for full duration, false if interrupted
func SleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	select {
	case <-ctx.Done():
		timer.Stop()
		return false
	case <-timer.C:
		return true
	}
}
