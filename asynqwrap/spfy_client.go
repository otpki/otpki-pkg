package asynqwrap

import (
	"context"
	"errors"

	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

type spfyClient struct{ broker *spfyBroker }

func (c *spfyClient) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return c.EnqueueContext(context.Background(), task, opts...)
}

func (c *spfyClient) EnqueueContext(
	ctx context.Context,
	task *asynq.Task,
	opts ...asynq.Option,
) (*asynq.TaskInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if task == nil {
		return nil, oops.New("task cannot be nil")
	}

	now := c.broker.now()
	opts = append(taskOptions(task), opts...)
	cfg := parseOptions(now, opts)
	rec := newTaskRecord(task, cfg, now)

	stateName := asynq.TaskStatePending.String()
	switch {
	case rec.processAt.After(now):
		stateName = asynq.TaskStateScheduled.String()
		rec.info.NextProcessAt = rec.processAt
	case rec.info.Group != "":
		stateName = asynq.TaskStateAggregating.String()
		rec.info.NextProcessAt = now
	default:
		rec.info.NextProcessAt = now
	}
	rec.info.State = taskStateFromString(stateName)
	doc := newSpfyTaskDoc(rec, stateName)
	if doc.NextProcessAt.IsZero() {
		doc.NextProcessAt = now
		doc.refreshUnixFields()
	}

	if err := c.broker.ensureQueue(ctx, rec.info.Queue); err != nil {
		return nil, err
	}

	for range 5 {
		if c.broker.supportsTransactions() && rec.uniqueKey == "" {
			taskSetDoc, taskSetETag, _, err := c.broker.getStringSetDoc(ctx, c.broker.queueTasksKey(rec.info.Queue))
			if err != nil {
				return nil, err
			}
			ops := []spfyStateOperation{
				// The queue-task index is an upserted derived document, not a uniqueness boundary.
				spfySetRequest(
					c.broker.queueTasksKey(rec.info.Queue),
					buildStringSetDoc(spfyKindTaskSet, taskSetDoc, rec.info.ID),
					taskSetETag,
					false,
					c.broker.stateMetadata(0),
				),
				spfySetRequest(c.broker.taskKey(rec.info.ID), doc, nil, true, c.broker.stateMetadata(0)),
			}
			txErr := c.broker.transaction(ctx, ops...)
			if txErr != nil {
				classified := c.classifyEnqueueConflict(ctx, rec, txErr)
				if errors.Is(classified, txErr) && isETagConflict(txErr) {
					continue
				}
				if errors.Is(classified, txErr) {
					_ = c.broker.deleteStateKey(ctx, c.broker.taskKey(rec.info.ID), nil)
					_ = c.broker.removeStringSetItems(
						ctx,
						c.broker.queueTasksKey(rec.info.Queue),
						rec.info.ID,
					)
				} else {
					return nil, classified
				}
			}
			if txErr == nil {
				if stateName == asynq.TaskStatePending.String() {
					_ = c.broker.notifyQueue(ctx, rec.info.Queue)
				}
				return cloneTaskInfo(rec.info), nil
			}
		}
		if err := c.broker.addStringSetItems(
			ctx,
			c.broker.queueTasksKey(rec.info.Queue),
			spfyKindTaskSet,
			rec.info.ID,
		); err != nil {
			if isETagConflict(err) {
				continue
			}
			return nil, err
		}
		if rec.uniqueKey != "" {
			uniqueDoc := &spfyUniqueDoc{
				Kind:      spfyKindUnique,
				Key:       rec.uniqueKey,
				TaskID:    rec.info.ID,
				ExpiresAt: rec.uniqueExpires,
			}
			if err := c.broker.applyStateOperation(
				ctx,
				spfySetRequest(
					c.broker.uniqueKey(rec.uniqueKey),
					uniqueDoc,
					nil,
					true,
					c.broker.stateMetadata(rec.uniqueExpires.Sub(now)),
				),
			); err != nil {
				return nil, c.classifyEnqueueConflict(ctx, rec, err)
			}
		}
		if err := c.broker.applyStateOperation(
			ctx,
			spfySetRequest(c.broker.taskKey(rec.info.ID), doc, nil, true, c.broker.stateMetadata(0)),
		); err != nil {
			if rec.uniqueKey != "" && !errors.Is(err, asynq.ErrDuplicateTask) {
				_ = c.broker.deleteStateKey(ctx, c.broker.uniqueKey(rec.uniqueKey), nil)
			}
			classified := c.classifyEnqueueConflict(ctx, rec, err)
			if errors.Is(classified, err) && isETagConflict(err) {
				continue
			}
			return nil, classified
		}
		if stateName == asynq.TaskStatePending.String() {
			_ = c.broker.notifyQueue(ctx, rec.info.Queue)
		}
		return cloneTaskInfo(rec.info), nil
	}
	return nil, oops.New("enqueue task failed after ETag retries")
}

func (c *spfyClient) classifyEnqueueConflict(ctx context.Context, rec *taskRecord, err error) error {
	if !isETagConflict(err) {
		return err
	}
	if rec.uniqueKey != "" {
		_, _, ok, getErr := c.broker.getState(ctx, c.broker.uniqueKey(rec.uniqueKey))
		if getErr == nil && ok {
			return asynq.ErrDuplicateTask
		}
	}
	if env, ok, getErr := c.broker.getTask(ctx, rec.info.ID); getErr == nil && ok && env != nil {
		return asynq.ErrTaskIDConflict
	}
	return err
}

func (c *spfyClient) Close() error { return nil }
func (c *spfyClient) Ping() error  { return c.broker.Ping() }
