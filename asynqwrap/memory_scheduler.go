package asynqwrap

import (
	"strconv"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/robfig/cron/v3"
	"github.com/samber/oops"
)

type memoryScheduler struct {
	broker *memoryBroker
	cron   *cron.Cron
	opts   *asynq.SchedulerOpts
	logger *leveledLogger

	mu       sync.Mutex
	entries  map[string]cron.EntryID
	started  bool
	shutdown bool
	done     chan struct{}
}

func newMemoryScheduler(b *memoryBroker, opts *asynq.SchedulerOpts) Scheduler {
	if opts == nil {
		opts = &asynq.SchedulerOpts{}
	}
	copied := *opts

	loc := copied.Location
	if loc == nil {
		loc = time.UTC
	}
	copied.Location = loc

	return &memoryScheduler{
		broker:  b,
		opts:    &copied,
		cron:    cron.New(cron.WithLocation(loc)),
		logger:  newLeveledLogger(copied.Logger, copied.LogLevel),
		entries: make(map[string]cron.EntryID),
		done:    make(chan struct{}),
	}
}

func (s *memoryScheduler) Ping() error { return nil }

func (s *memoryScheduler) Register(spec string, task *asynq.Task, opts ...asynq.Option) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shutdown {
		return "", oops.New("scheduler is shut down")
	}

	id, err := s.cron.AddFunc(spec, func() {
		s.runScheduledTask(task, opts)
	})
	if err != nil {
		return "", err
	}

	entryID := strconv.FormatInt(int64(id), 10)
	s.entries[entryID] = id
	return entryID, nil
}

func (s *memoryScheduler) Start() error {
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

func (s *memoryScheduler) Run() error {
	if err := s.Start(); err != nil {
		return err
	}
	<-s.done
	return nil
}

func (s *memoryScheduler) Shutdown() {
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

func (s *memoryScheduler) Unregister(entryID string) error {
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

func (s *memoryScheduler) runScheduledTask(task *asynq.Task, opts []asynq.Option) {
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

type memoryInspector struct {
	broker *memoryBroker
}

func (i *memoryInspector) recurringStateStore() recurringStateStore {
	return i.broker.recurringState
}

func (i *memoryInspector) Close() error { return nil }

func (i *memoryInspector) Groups(queue string) ([]*asynq.GroupInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.groupInfosLocked(queue), nil
}

func (i *memoryInspector) GetQueueInfo(queue string) (*asynq.QueueInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.queueInfoLocked(queue), nil
}

func (i *memoryInspector) GetTaskInfo(queue, id string) (*asynq.TaskInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()

	rec := i.broker.findTaskLocked(queue, id)
	if rec == nil {
		return nil, oops.Errorf("task %q not found in queue %q", id, queue)
	}
	return cloneTaskInfo(rec.info), nil
}

func (i *memoryInspector) ListArchivedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.listTasksByStateLocked(queue, asynq.TaskStateArchived, opts...), nil
}

func (i *memoryInspector) ListCompletedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.listTasksByStateLocked(queue, asynq.TaskStateCompleted, opts...), nil
}

func (i *memoryInspector) ListActiveTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.listTasksByStateLocked(queue, asynq.TaskStateActive, opts...), nil
}

func (i *memoryInspector) ListPendingTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.listTasksByStateLocked(queue, asynq.TaskStatePending, opts...), nil
}

func (i *memoryInspector) ListRetryTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.listTasksByStateLocked(queue, asynq.TaskStateRetry, opts...), nil
}

func (i *memoryInspector) ListScheduledTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.listTasksByStateLocked(queue, asynq.TaskStateScheduled, opts...), nil
}

func (i *memoryInspector) ArchiveTask(queue, id string) error {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.archiveTaskLocked(queue, id)
}

func (i *memoryInspector) DeleteTask(queue, id string) error {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.deleteTaskLocked(queue, id)
}

func (i *memoryInspector) DeleteAllArchivedTasks(queue string) (int, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.deleteAllTasksInStateLocked(queue, asynq.TaskStateArchived), nil
}

func (i *memoryInspector) DeleteAllRetryTasks(queue string) (int, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.deleteAllTasksInStateLocked(queue, asynq.TaskStateRetry), nil
}

func (i *memoryInspector) PauseQueue(queue string) error {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	i.broker.getOrCreateQueueLocked(queue).paused = true
	return nil
}

func (i *memoryInspector) Queues() ([]string, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.queueNamesLocked(), nil
}

func (i *memoryInspector) RunTask(queue, id string) error {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.runTaskLocked(queue, id)
}

func (i *memoryInspector) RunAllArchivedTasks(queue string) (int, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.runAllTasksInStateLocked(queue, asynq.TaskStateArchived), nil
}

func (i *memoryInspector) RunAllRetryTasks(queue string) (int, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.runAllTasksInStateLocked(queue, asynq.TaskStateRetry), nil
}

func (i *memoryInspector) RunAllScheduledTasks(queue string) (int, error) {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	return i.broker.runAllTasksInStateLocked(queue, asynq.TaskStateScheduled), nil
}

func (i *memoryInspector) UnpauseQueue(queue string) error {
	i.broker.mu.Lock()
	defer i.broker.mu.Unlock()
	i.broker.getOrCreateQueueLocked(queue).paused = false
	i.broker.cond.Broadcast()
	return nil
}
