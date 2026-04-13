package asynqwrap

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

const (
	defaultRecurringTaskQueue  = "default"
	recurringTaskKeyHeader     = "asynqwrap-recurring-key"
	recurringTaskQueueHeader   = "asynqwrap-recurring-queue"
	recurringTaskDelayHeader   = "asynqwrap-recurring-delay"
	recurringTaskVersionHeader = "asynqwrap-recurring-version"
)

// RecurringTaskSpec describes the desired schedule for a self-rescheduling task.
//
// Key should uniquely identify the logical recurring task across your application.
type RecurringTaskSpec[K comparable] struct {
	Key      K
	Task     *asynq.Task
	Interval time.Duration
	Queue    string
}

// DeleteRecurringTask removes pending, scheduled, and retry tasks for the given key.
//
// Active tasks are not deleted, but tasks already in progress will not re-enqueue
// themselves if they were superseded or deleted while running.
func DeleteRecurringTask[K comparable](ctx context.Context, inspector Inspector, key K) (int, error) {
	store, err := recurringStateStoreFromInspector(inspector)
	if err != nil {
		return 0, err
	}

	encodedKey, err := encodeRecurringTaskKey(key)
	if err != nil {
		return 0, err
	}
	if err := store.Delete(ctx, encodedKey); err != nil {
		return 0, err
	}

	return deleteQueuedRecurringTasks(inspector, key)
}

// UpsertRecurringTask deletes existing queued copies of the recurring task.
//
// The initial insert enqueues the task immediately. Subsequent upserts enqueue
// the next run at now + interval. After a successful run, the middleware
// schedules the next run using the same interval.
func UpsertRecurringTask[K comparable](ctx context.Context, client Client, inspector Inspector, spec RecurringTaskSpec[K]) (*asynq.TaskInfo, error) {
	if client == nil {
		return nil, oops.Errorf("client cannot be nil")
	}

	store, err := recurringStateStoreFromInspector(inspector)
	if err != nil {
		return nil, err
	}

	spec, err = normalizeRecurringTaskSpec(spec)
	if err != nil {
		return nil, err
	}

	encodedKey, err := encodeRecurringTaskKey(spec.Key)
	if err != nil {
		return nil, err
	}

	version := uuid.NewString()
	existed, err := store.Upsert(ctx, encodedKey, version)
	if err != nil {
		return nil, err
	}

	if _, err := deleteQueuedRecurringTasks(inspector, spec.Key); err != nil {
		return nil, err
	}

	task, err := cloneTaskWithRecurringState(spec, version)
	if err != nil {
		return nil, err
	}

	opts := []asynq.Option{asynq.Queue(spec.Queue)}
	if existed {
		opts = append(opts, asynq.ProcessIn(spec.Interval))
	}
	return client.EnqueueContext(ctx, task, opts...)
}

// SelfReschedulingTaskMiddleware re-enqueues the next run after a successful handler call.
//
// The middleware only applies to tasks created by UpsertRecurringTask.
func SelfReschedulingTaskMiddleware(client Client, inspector Inspector) asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			if task == nil {
				return next.ProcessTask(ctx, task)
			}

			state, ok, err := decodeRecurringTaskState(task.Headers())
			if err != nil {
				return err
			}
			if !ok {
				return next.ProcessTask(ctx, task)
			}

			if err := next.ProcessTask(ctx, task); err != nil {
				return err
			}

			store, err := recurringStateStoreFromInspector(inspector)
			if err != nil {
				return err
			}

			key, ok := rawRecurringTaskKey(task.Headers())
			if !ok {
				return nil
			}
			version, ok := recurringTaskVersion(task.Headers())
			if !ok {
				return nil
			}

			currentVersion, ok, err := store.Get(ctx, key)
			if err != nil {
				return err
			}
			if !ok || currentVersion != version {
				return nil
			}

			nextTask, err := cloneTaskWithHeaders(task)
			if err != nil {
				return err
			}
			_, err = client.EnqueueContext(ctx, nextTask, asynq.Queue(state.Queue), asynq.ProcessIn(state.Interval))
			return err
		})
	}
}

func normalizeRecurringTaskSpec[K comparable](spec RecurringTaskSpec[K]) (RecurringTaskSpec[K], error) {
	if spec.Task == nil {
		return spec, oops.Errorf("task cannot be nil")
	}
	if spec.Interval <= 0 {
		return spec, oops.Errorf("interval must be > 0")
	}
	if spec.Queue == "" {
		spec.Queue = defaultRecurringTaskQueue
	}
	return spec, nil
}

func cloneTaskWithRecurringState[K comparable](spec RecurringTaskSpec[K], version string) (*asynq.Task, error) {
	encodedKey, err := encodeRecurringTaskKey(spec.Key)
	if err != nil {
		return nil, err
	}
	delay, err := encodeRecurringTaskDelay(spec.Interval)
	if err != nil {
		return nil, err
	}

	headers := make(map[string]string, len(spec.Task.Headers())+4)
	maps.Copy(headers, spec.Task.Headers())
	headers[recurringTaskKeyHeader] = encodedKey
	headers[recurringTaskQueueHeader] = spec.Queue
	headers[recurringTaskDelayHeader] = delay
	headers[recurringTaskVersionHeader] = version

	return asynq.NewTaskWithHeaders(spec.Task.Type(), spec.Task.Payload(), headers, taskOptions(spec.Task)...), nil
}

func encodeRecurringTaskKey[K comparable](key K) (string, error) {
	body, err := json.Marshal(key)
	if err != nil {
		return "", oops.Errorf("marshal recurring task key: %w", err)
	}
	return string(body), nil
}

func encodeRecurringTaskDelay(delay time.Duration) (string, error) {
	if delay <= 0 {
		return "", oops.Errorf("interval must be > 0")
	}
	return delay.String(), nil
}

type recurringTaskSchedule struct {
	Queue    string
	Interval time.Duration
}

func rawRecurringTaskKey(headers map[string]string) (string, bool) {
	if len(headers) == 0 {
		return "", false
	}
	body, ok := headers[recurringTaskKeyHeader]
	if !ok || body == "" {
		return "", false
	}
	return body, true
}

func recurringTaskKey[K comparable](headers map[string]string) (key K, ok bool, err error) {
	body, ok := rawRecurringTaskKey(headers)
	if !ok {
		return key, false, nil
	}
	if err := json.Unmarshal([]byte(body), &key); err != nil {
		return key, false, oops.Errorf("decode recurring task key: %w", err)
	}
	return key, true, nil
}

func recurringTaskVersion(headers map[string]string) (string, bool) {
	if len(headers) == 0 {
		return "", false
	}
	version, ok := headers[recurringTaskVersionHeader]
	if !ok || version == "" {
		return "", false
	}
	return version, true
}

func decodeRecurringTaskState(headers map[string]string) (state recurringTaskSchedule, ok bool, err error) {
	if len(headers) == 0 {
		return state, false, nil
	}

	queue, ok := headers[recurringTaskQueueHeader]
	if !ok || queue == "" {
		return state, false, nil
	}
	delayText, ok := headers[recurringTaskDelayHeader]
	if !ok || delayText == "" {
		return state, false, nil
	}
	delay, err := time.ParseDuration(delayText)
	if err != nil {
		return state, false, oops.Errorf("decode recurring task delay: %w", err)
	}
	return recurringTaskSchedule{Queue: queue, Interval: delay}, true, nil
}

func cloneTaskWithHeaders(task *asynq.Task) (*asynq.Task, error) {
	if task == nil {
		return nil, oops.Errorf("task cannot be nil")
	}

	headers := make(map[string]string, len(task.Headers()))
	maps.Copy(headers, task.Headers())
	return asynq.NewTaskWithHeaders(task.Type(), task.Payload(), headers, taskOptions(task)...), nil
}

func deleteQueuedRecurringTasks[K comparable](inspector Inspector, key K) (int, error) {
	if inspector == nil {
		return 0, oops.Errorf("inspector cannot be nil")
	}

	queues, err := inspector.Queues()
	if err != nil {
		return 0, oops.Errorf("list queues: %w", err)
	}
	queues = append(queues, defaultRecurringTaskQueue)

	seen := make(map[string]struct{}, len(queues))
	var deleted int
	for _, queue := range queues {
		if _, ok := seen[queue]; ok {
			continue
		}
		seen[queue] = struct{}{}

		n, err := deleteRecurringTasksInQueue(inspector, queue, key)
		if err != nil {
			return deleted, err
		}
		deleted += n
	}

	return deleted, nil
}

func deleteRecurringTasksInQueue[K comparable](inspector Inspector, queue string, key K) (int, error) {
	var deleted int
	listFns := []func(string) ([]*asynq.TaskInfo, error){
		inspector.ListPendingTasks,
		inspector.ListScheduledTasks,
		inspector.ListRetryTasks,
	}

	for _, listFn := range listFns {
		tasks, err := listFn(queue)
		if err != nil {
			if strings.Contains(err.Error(), "queue not found") {
				continue
			}
			return deleted, oops.Errorf("list recurring tasks in queue %q: %w", queue, err)
		}

		for _, task := range tasks {
			taskKey, ok, err := recurringTaskKey[K](task.Headers)
			if err != nil || !ok || taskKey != key {
				continue
			}

			if err := inspector.DeleteTask(task.Queue, task.ID); err != nil {
				return deleted, oops.Errorf("delete recurring task %q from queue %q: %w", task.ID, task.Queue, err)
			}
			deleted++
		}
	}

	return deleted, nil
}
