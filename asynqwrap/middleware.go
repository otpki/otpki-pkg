package asynqwrap

import (
	"context"
	"slices"
	"sync"

	"github.com/hibiken/asynq"
)

type clientBackend interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
	Ping() error
}

type clientAdapter struct {
	backend clientBackend

	mu         sync.RWMutex
	middleware []EnqueueMiddleware
}

func newClientAdapter(backend clientBackend) Client {
	return &clientAdapter{backend: backend}
}

func (c *clientAdapter) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return c.EnqueueContext(context.Background(), task, opts...)
}

func (c *clientAdapter) EnqueueContext(
	ctx context.Context,
	task *asynq.Task,
	opts ...asynq.Option,
) (*asynq.TaskInfo, error) {
	return c.enqueue(ctx, task, opts...)
}

func (c *clientAdapter) Use(middleware ...EnqueueMiddleware) {
	if len(middleware) == 0 {
		return
	}

	c.mu.Lock()
	c.middleware = append(c.middleware, middleware...)
	c.mu.Unlock()
}

func (c *clientAdapter) Close() error { return c.backend.Close() }

func (c *clientAdapter) Ping() error { return c.backend.Ping() }

func (c *clientAdapter) enqueue(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	next := func(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
		return c.backend.EnqueueContext(ctx, task, opts...)
	}

	middleware := c.snapshot()
	for _, v := range slices.Backward(middleware) {
		next = v(next)
	}

	return next(ctx, task, opts...)
}

func (c *clientAdapter) snapshot() []EnqueueMiddleware {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]EnqueueMiddleware, len(c.middleware))
	copy(out, c.middleware)
	return out
}

type serverBackend interface {
	Ping() error
	Run(handler asynq.Handler) error
	Start(handler asynq.Handler) error
	Stop()
	Shutdown()
}

type serverAdapter struct {
	backend serverBackend

	mu         sync.RWMutex
	middleware []asynq.MiddlewareFunc
}

func newServerAdapter(backend serverBackend) Server {
	return &serverAdapter{backend: backend}
}

func (s *serverAdapter) Ping() error { return s.backend.Ping() }

func (s *serverAdapter) Use(middleware ...asynq.MiddlewareFunc) {
	if len(middleware) == 0 {
		return
	}

	s.mu.Lock()
	s.middleware = append(s.middleware, middleware...)
	s.mu.Unlock()
}

func (s *serverAdapter) Run(handler asynq.Handler) error {
	return s.backend.Run(s.wrap(handler))
}

func (s *serverAdapter) Start(handler asynq.Handler) error {
	return s.backend.Start(s.wrap(handler))
}

func (s *serverAdapter) Stop() { s.backend.Stop() }

func (s *serverAdapter) Shutdown() { s.backend.Shutdown() }

func (s *serverAdapter) wrap(handler asynq.Handler) asynq.Handler {
	wrapped := handler
	middleware := s.snapshot()
	for _, v := range slices.Backward(middleware) {
		wrapped = v(wrapped)
	}
	return wrapped
}

func (s *serverAdapter) snapshot() []asynq.MiddlewareFunc {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]asynq.MiddlewareFunc, len(s.middleware))
	copy(out, s.middleware)
	return out
}
