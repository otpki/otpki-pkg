package asynqwrap

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

type memoryClient struct {
	broker *memoryBroker
}

func (c *memoryClient) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return c.EnqueueContext(context.Background(), task, opts...)
}

func (c *memoryClient) EnqueueContext(
	ctx context.Context,
	task *asynq.Task,
	opts ...asynq.Option,
) (*asynq.TaskInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now()
	opts = append(taskOptions(task), opts...)
	cfg := parseOptions(now, opts)
	rec := newTaskRecord(task, cfg, now)

	b := c.broker
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, oops.New("memory broker is closed")
	}

	b.expireLocked(now)
	if err := b.validateTaskRecordLocked(rec, now); err != nil {
		return nil, err
	}

	b.tasks[rec.info.ID] = rec
	b.enqueueRecordLocked(rec)
	return cloneTaskInfo(rec.info), nil
}

func (c *memoryClient) Close() error { return nil }

func (c *memoryClient) Ping() error { return nil }

func (b *memoryBroker) validateTaskRecordLocked(rec *taskRecord, now time.Time) error {
	if err := b.checkTaskIDConflictLocked(rec.info.ID); err != nil {
		return err
	}
	if err := b.checkUniqueConflictLocked(rec, now); err != nil {
		return err
	}
	return nil
}

func (b *memoryBroker) checkTaskIDConflictLocked(taskID string) error {
	if taskID == "" {
		return nil
	}
	if _, exists := b.tasks[taskID]; exists {
		return asynq.ErrTaskIDConflict
	}
	if _, exists := b.completed[taskID]; exists {
		return asynq.ErrTaskIDConflict
	}
	if _, exists := b.archived[taskID]; exists {
		return asynq.ErrTaskIDConflict
	}
	return nil
}

func (b *memoryBroker) checkUniqueConflictLocked(rec *taskRecord, now time.Time) error {
	if rec.uniqueKey == "" {
		return nil
	}
	if expires, ok := b.uniqueKeys[rec.uniqueKey]; ok && now.Before(expires) {
		return asynq.ErrDuplicateTask
	}
	b.uniqueKeys[rec.uniqueKey] = rec.uniqueExpires
	return nil
}
