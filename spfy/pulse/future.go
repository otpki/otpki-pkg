package pulse

import "context"

type Result[T any] struct {
	Value T
	Err   error
}

type Future[T any] struct {
	ch <-chan Result[T]
}

func NewFuture[T any](ch <-chan Result[T]) Future[T] {
	return Future[T]{ch: ch}
}

func SucceededFuture[T any](value T) Future[T] {
	ch := make(chan Result[T], 1)
	ch <- Result[T]{Value: value}
	close(ch)
	return Future[T]{ch: ch}
}

func FailedFuture[T any](err error) Future[T] {
	ch := make(chan Result[T], 1)
	ch <- Result[T]{Err: err}
	close(ch)
	return Future[T]{ch: ch}
}

func (f Future[T]) Done() <-chan Result[T] {
	return f.ch
}

func (f Future[T]) Await(ctx context.Context) (T, error) {
	var zero T
	if f.ch == nil {
		return zero, ErrNoResult
	}
	select {
	case res, ok := <-f.ch:
		if !ok {
			return zero, ErrNoResult
		}
		return res.Value, res.Err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}
