package asynqwrap

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

type taskConfig struct {
	queue       string
	taskID      string
	group       string
	maxRetry    int
	maxRetrySet bool
	timeout     time.Duration
	deadline    time.Time
	uniqueTTL   time.Duration
	processAt   time.Time
	retention   time.Duration
}

func parseOptions(now time.Time, opts []asynq.Option) taskConfig {
	cfg := taskConfig{maxRetry: defaultMaxRetry}
	for _, opt := range opts {
		switch opt.Type() {
		case asynq.MaxRetryOpt:
			cfg.maxRetry = opt.Value().(int)
			cfg.maxRetrySet = true
		case asynq.QueueOpt:
			cfg.queue = opt.Value().(string)
		case asynq.TimeoutOpt:
			cfg.timeout = opt.Value().(time.Duration)
		case asynq.DeadlineOpt:
			cfg.deadline = opt.Value().(time.Time)
		case asynq.UniqueOpt:
			cfg.uniqueTTL = opt.Value().(time.Duration)
		case asynq.ProcessAtOpt:
			cfg.processAt = opt.Value().(time.Time)
		case asynq.ProcessInOpt:
			cfg.processAt = now.Add(opt.Value().(time.Duration))
		case asynq.TaskIDOpt:
			cfg.taskID = opt.Value().(string)
		case asynq.RetentionOpt:
			cfg.retention = opt.Value().(time.Duration)
		case asynq.GroupOpt:
			cfg.group = opt.Value().(string)
		}
	}
	return cfg
}

func newTaskRecord(task *asynq.Task, cfg taskConfig, now time.Time) *taskRecord {
	task = cloneTask(task)

	queue := cfg.queue
	if queue == "" {
		queue = defaultRecurringTaskQueue
	}

	id := cfg.taskID
	if id == "" {
		id = uuid.NewString()
	}

	rec := &taskRecord{
		task:      task,
		createdAt: now,
		processAt: cfg.processAt,
		retention: cfg.retention,
		info: &asynq.TaskInfo{
			ID:            id,
			Queue:         queue,
			Type:          task.Type(),
			Payload:       append([]byte(nil), task.Payload()...),
			Headers:       cloneHeaders(task.Headers()),
			MaxRetry:      cfg.maxRetry,
			Timeout:       cfg.timeout,
			Deadline:      cfg.deadline,
			Group:         cfg.group,
			NextProcessAt: cfg.processAt,
		},
	}

	if rec.info.MaxRetry == 0 && !cfg.maxRetrySet {
		rec.info.MaxRetry = defaultMaxRetry
	}
	if cfg.uniqueTTL > 0 {
		rec.uniqueExpires = now.Add(cfg.uniqueTTL)
		rec.uniqueKey = uniqueKey(queue, task)
	}
	return rec
}

func normalizeConfig(cfg asynq.Config) asynq.Config {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if len(cfg.Queues) == 0 {
		cfg.Queues = map[string]int{defaultRecurringTaskQueue: 1}
	}
	return cfg
}

func retryDelay(cfg asynq.Config, retry int, taskErr error, task *asynq.Task) time.Duration {
	if cfg.RetryDelayFunc != nil {
		return cfg.RetryDelayFunc(retry, taskErr, task)
	}

	delay := time.Second * time.Duration(1<<(retry-1))
	if delay > 30*time.Minute {
		return 30 * time.Minute
	}
	return delay
}

func taskDeadline(info *asynq.TaskInfo, now time.Time) (time.Time, bool) {
	switch {
	case info.Timeout > 0 && !info.Deadline.IsZero():
		timeoutDeadline := now.Add(info.Timeout)
		if timeoutDeadline.Before(info.Deadline) {
			return timeoutDeadline, true
		}
		return info.Deadline, true
	case info.Timeout > 0:
		return now.Add(info.Timeout), true
	case !info.Deadline.IsZero():
		return info.Deadline, true
	default:
		return time.Time{}, false
	}
}

func cloneTaskInfo(info *asynq.TaskInfo) *asynq.TaskInfo {
	if info == nil {
		return nil
	}
	out := *info
	out.Payload = append([]byte(nil), info.Payload...)
	out.Headers = cloneHeaders(info.Headers)
	out.Result = append([]byte(nil), info.Result...)
	return &out
}

func cloneTask(task *asynq.Task) *asynq.Task {
	if task == nil {
		return nil
	}
	return asynq.NewTaskWithHeaders(
		task.Type(),
		append([]byte(nil), task.Payload()...),
		cloneHeaders(task.Headers()),
		taskOptions(task)...,
	)
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func uniqueKey(queue string, task *asynq.Task) string {
	sum := sha256.Sum256(append([]byte(queue+":"+task.Type()+":"), task.Payload()...))
	return hex.EncodeToString(sum[:])
}

func collectTasks(records []*taskRecord) []*asynq.Task {
	out := make([]*asynq.Task, 0, len(records))
	for _, rec := range records {
		out = append(out, rec.task)
	}
	return out
}
