package pulse

import (
	"context"

	"github.com/samber/oops"
)

// Future represents a future value that can be resolved asynchronously
type Future[T any] struct {
	ResultCh chan T
	ErrCh    chan error
	Ctx      context.Context
	Done     chan struct{}
}

func (f *Future[T]) Resolve() (T, error) {
	defer close(f.Done)
	select {
	case result, ok := <-f.ResultCh:
		if !ok {
			// channel was closed without sending a value
			var zero T
			return zero, oops.In(ServiceName).New("response channel closed before receiving result")
		}
		return result, nil
	case err := <-f.ErrCh:
		var zero T
		return zero, err
	case <-f.Ctx.Done():
		var zero T
		return zero, f.Ctx.Err()
	}
}

func (f *Future[T]) ResultChannel() <-chan T {
	return f.ResultCh
}

func (f *Future[T]) ErrorChannel() <-chan error {
	return f.ErrCh
}

// Envelope is the actual message sent across Pulsar. It is abstractly represented by an RPC resp/err combo
type Envelope struct {
	Payload  []byte              `json:"payload"`
	Metadata map[string][]string `json:"metadata,omitempty"`
	Error    *EnvelopeError      `json:"error,omitempty"`
}

type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
