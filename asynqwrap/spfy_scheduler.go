package asynqwrap

import (
	"strconv"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/robfig/cron/v3"
	"github.com/samber/oops"
)

type spfyScheduler struct {
	broker *spfyBroker
	cron   *cron.Cron
	opts   *asynq.SchedulerOpts
	logger *leveledLogger

	mu       sync.Mutex
	entries  map[string]cron.EntryID
	started  bool
	shutdown bool
	done     chan struct{}
}

func newSpfyScheduler(b *spfyBroker, opts *asynq.SchedulerOpts) Scheduler {
	if opts == nil {
		opts = &asynq.SchedulerOpts{}
	}
	copied := *opts
	loc := copied.Location
	if loc == nil {
		loc = time.UTC
	}
	copied.Location = loc
	return &spfyScheduler{
		broker:  b,
		opts:    &copied,
		cron:    cron.New(cron.WithLocation(loc)),
		logger:  newLeveledLogger(copied.Logger, copied.LogLevel),
		entries: make(map[string]cron.EntryID),
		done:    make(chan struct{}),
	}
}

func (s *spfyScheduler) Ping() error { return s.broker.Ping() }

func (s *spfyScheduler) Register(spec string, task *asynq.Task, opts ...asynq.Option) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdown {
		return "", oops.New("scheduler is shut down")
	}
	id, err := s.cron.AddFunc(spec, func() { s.runScheduledTask(task, opts) })
	if err != nil {
		return "", err
	}
	entryID := strconv.FormatInt(int64(id), 10)
	s.entries[entryID] = id
	return entryID, nil
}

func (s *spfyScheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdown {
		return oops.New("scheduler is shut down")
	}
	if s.started {
		return nil
	}
	s.cron.Start()
	s.started = true
	s.logger.Infof("Scheduler timezone is set to %v", s.opts.Location)
	return nil
}

func (s *spfyScheduler) Run() error {
	if err := s.Start(); err != nil {
		return err
	}
	<-s.done
	return nil
}

func (s *spfyScheduler) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdown {
		return
	}
	s.shutdown = true
	ctx := s.cron.Stop()
	close(s.done)
	<-ctx.Done()
}

func (s *spfyScheduler) Unregister(entryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.entries[entryID]
	if !ok {
		return oops.Errorf("unknown scheduler entry %q", entryID)
	}
	s.cron.Remove(id)
	delete(s.entries, entryID)
	return nil
}

func (s *spfyScheduler) runScheduledTask(task *asynq.Task, opts []asynq.Option) {
	if s.opts.PreEnqueueFunc != nil {
		s.opts.PreEnqueueFunc(task, opts)
	}
	info, err := s.broker.Client().Enqueue(task, opts...)
	if err == nil {
		s.logger.Debugf("scheduler enqueued a task: %+v", info)
	}
	if s.opts.PostEnqueueFunc != nil {
		s.opts.PostEnqueueFunc(info, err)
	}
	if err != nil {
		s.logger.Errorf("scheduler could not enqueue task %q: %v", task.Type(), err)
	}
}
