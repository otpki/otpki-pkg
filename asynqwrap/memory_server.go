package asynqwrap

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

type memoryServer struct {
	broker   *memoryBroker
	cfg      asynq.Config
	serverID uint64
	logger   *leveledLogger

	mu       sync.Mutex
	started  bool
	stopped  bool
	done     chan struct{}
	stopPull chan struct{}
	wg       sync.WaitGroup
	rrIndex  int
}

func (s *memoryServer) Ping() error { return nil }

func (s *memoryServer) Start(handler asynq.Handler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	s.started = true
	s.stopPull = make(chan struct{})

	for range s.workerCount() {
		s.wg.Add(1)
		go s.workerLoop(handler)
	}
	return nil
}

func (s *memoryServer) Run(handler asynq.Handler) error {
	if err := s.Start(handler); err != nil {
		return err
	}
	<-s.done
	return nil
}

func (s *memoryServer) Stop() {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopPull)
	s.mu.Unlock()

	s.broker.mu.Lock()
	s.broker.cond.Broadcast()
	s.broker.mu.Unlock()

	s.wg.Wait()
	close(s.done)
}

func (s *memoryServer) Shutdown() {
	s.Stop()
}

func (s *memoryServer) workerCount() int {
	if s.cfg.Concurrency <= 0 {
		return 1
	}
	return s.cfg.Concurrency
}

func (s *memoryServer) workerLoop(handler asynq.Handler) {
	defer s.wg.Done()
	for {
		rec := s.nextTask()
		if rec == nil {
			return
		}
		s.processTask(handler, rec)
	}
}

func (s *memoryServer) nextTask() *taskRecord {
	b := s.broker
	b.mu.Lock()
	defer b.mu.Unlock()

	for {
		select {
		case <-s.stopPull:
			return nil
		default:
		}

		now := time.Now()
		b.expireLocked(now)
		b.flushGroupsLocked(now)

		if rec := b.popReadyLocked(s.cfg, &s.rrIndex); rec != nil {
			return rec
		}

		b.cond.Wait()
	}
}

func (s *memoryServer) processTask(handler asynq.Handler, rec *taskRecord) {
	ctx, cancel := s.taskContext(rec)
	defer cancel()
	ctx = withTaskResultWriter(
		ctx,
		rec.info.ID,
		rec.info.Queue,
		&memoryTaskResultWriter{broker: s.broker, rec: rec},
	)

	err := s.runHandler(ctx, handler, rec)
	s.finishProcessing(ctx, rec, err)
}

func (s *memoryServer) taskContext(rec *taskRecord) (context.Context, context.CancelFunc) {
	base := context.Background()
	if s.cfg.BaseContext != nil {
		base = s.cfg.BaseContext()
	}
	if deadline, ok := taskDeadline(rec.info, time.Now()); ok {
		return context.WithDeadline(base, deadline)
	}
	return base, func() {}
}

func (s *memoryServer) runHandler(ctx context.Context, handler asynq.Handler, rec *taskRecord) error {
	errCh := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := oops.Errorf("panic recovered: %v\n%s", r, debug.Stack())
				s.logger.Errorf("%v", err)
				errCh <- err
			}
		}()
		errCh <- handler.ProcessTask(ctx, rec.task)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *memoryServer) finishProcessing(ctx context.Context, rec *taskRecord, err error) {
	now := time.Now()

	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()

	delete(s.broker.active, rec.info.ID)

	if err == nil || errors.Is(err, asynq.RevokeTask) {
		s.broker.finishTaskLocked(rec, now)
		return
	}

	s.broker.failTaskLocked(ctx, s.cfg, rec, err, now)
}

type memoryTaskResultWriter struct {
	broker *memoryBroker
	rec    *taskRecord
}

func (w *memoryTaskResultWriter) WriteTaskResult(ctx context.Context, data []byte) (int, error) {
	select {
	case <-ctx.Done():
		return 0, oops.Errorf("failed to write task result: %w", ctx.Err())
	default:
	}

	w.broker.mu.Lock()
	w.rec.info.Result = append([]byte(nil), data...)
	w.broker.mu.Unlock()
	return len(data), nil
}
