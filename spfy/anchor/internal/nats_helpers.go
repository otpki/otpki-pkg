package internal

import (
	"context"
	"time"
)

func NATSDeadlineContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	if timeout <= 0 {
		timeout = DefaultNATSOperationTimeout
	}
	return context.WithTimeout(ctx, timeout)
}
