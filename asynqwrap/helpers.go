package asynqwrap

import (
	"time"

	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

// EnqueueNow is a convenience wrapper around Client.Enqueue.
func EnqueueNow(client Client, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return client.Enqueue(task, opts...)
}

// EnqueueIn enqueues a task to run after the provided interval.
func EnqueueIn(client Client, delay time.Duration, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	opts = append(opts, asynq.ProcessIn(delay))
	return client.Enqueue(task, opts...)
}

// EnqueueAt enqueues a task to run at the provided time.
func EnqueueAt(client Client, at time.Time, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	opts = append(opts, asynq.ProcessAt(at))
	return client.Enqueue(task, opts...)
}

// CronSpecEvery converts a time.Duration into an asynq "@every <duration>" spec.
func CronSpecEvery(interval time.Duration) (string, error) {
	if interval <= 0 {
		return "", oops.Errorf("interval must be > 0")
	}
	return "@every " + interval.String(), nil
}

// RegisterEvery registers a recurring task using a time.Duration interval.
func RegisterEvery(s Scheduler, interval time.Duration, task *asynq.Task, opts ...asynq.Option) (string, error) {
	spec, err := CronSpecEvery(interval)
	if err != nil {
		return "", err
	}
	return s.Register(spec, task, opts...)
}
