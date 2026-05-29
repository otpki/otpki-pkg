package asynqwrap

import (
	"context"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
	"github.com/otpki/otpki-pkg/config"
	"github.com/redis/go-redis/v9"
	"github.com/samber/oops"
)

// EnqueueFunc is the function shape wrapped by enqueue middleware.
//
// Middleware can observe or modify the context, task headers, and asynq options
// before delegating to the next enqueue function.
type EnqueueFunc func(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)

// EnqueueMiddleware wraps task publication through Client.Enqueue and
// Client.EnqueueContext.
//
// Use enqueue middleware for producer-side concerns such as tracing, metrics,
// logging, or header propagation.
type EnqueueMiddleware func(next EnqueueFunc) EnqueueFunc

type Mode string

const (
	// ModeRedis uses real asynq storage and coordination through Redis.
	ModeRedis Mode = "redis"
	// ModeMemory uses the in-process memory backend.
	ModeMemory Mode = "memory"
	// ModeSpfy uses Spfy components-contrib state and pub/sub components.
	// State is the durable source of truth, pub/sub is used as a wake-up signal.
	ModeSpfy Mode = "spfy"
)

// Config selects the backend and, for Redis mode, how Redis is provided.
type Config struct {
	// Mode chooses the backend implementation.
	Mode Mode
	// RuntimeConfig provides the app keyspace used to namespace asynqwrap-owned
	// Redis keys, Spfy state keys, and Spfy pub/sub topics.
	RuntimeConfig config.Config
	// Redis provides asynq connection options when the wrapper should create Redis clients itself.
	Redis asynq.RedisConnOpt
	// RedisClient reuses an existing go-redis client or pool for Redis mode.
	RedisClient redis.UniversalClient

	Spfy SpfyConfig
}

// Client enqueues tasks for immediate or future execution.
type Client interface {
	// Enqueue adds a task using the provided asynq options.
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	// EnqueueContext adds a task and allows cancellation before the enqueue completes.
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	// Use registers enqueue middleware for subsequent Enqueue and EnqueueContext calls.
	//
	// This is an asynqwrap-specific extension; upstream asynq.Client does not
	// expose a middleware registration method.
	// Middleware runs in registration order. Call Use during setup before sharing
	// the client widely.
	Use(middleware ...EnqueueMiddleware)
	// Close releases client resources when the backend requires it.
	Close() error
	// Ping verifies that the client can reach its backend.
	Ping() error
}

// Server processes tasks with normal asynq handlers.
type Server interface {
	// Ping verifies that the server can reach its backend.
	Ping() error
	// Use registers handler middleware that wraps the handler passed to Start or Run.
	//
	// This is an asynqwrap-specific extension; upstream asynq.Server does not
	// expose a middleware registration method.
	// Middleware runs in registration order. Call Use before Start or Run.
	Use(middleware ...asynq.MiddlewareFunc)
	// Run starts processing and blocks until the server stops.
	Run(handler asynq.Handler) error
	// Start begins processing in the background.
	Start(handler asynq.Handler) error
	// Stop stops pulling new tasks.
	Stop()
	// Shutdown stops the server and waits for workers to exit.
	Shutdown()
}

// Scheduler manages recurring task registration and execution.
type Scheduler interface {
	// Ping verifies that the scheduler can reach its backend.
	Ping() error
	// Register creates a recurring task from a cron spec or @every expression.
	Register(spec string, task *asynq.Task, opts ...asynq.Option) (string, error)
	// Run starts the scheduler and blocks until it is shut down.
	Run() error
	// Start begins the scheduler in the background.
	Start() error
	// Shutdown stops the scheduler.
	Shutdown()
	// Unregister removes a recurring task using the entry ID returned by Register.
	Unregister(entryID string) error
}

// PeriodicTaskManager manages dynamic periodic task registration using the
// same lifecycle methods as upstream asynq.
type PeriodicTaskManager interface {
	Start() error
	Run() error
	Shutdown()
}

// PeriodicTaskManagerOpts mirrors upstream asynq.PeriodicTaskManagerOpts and
// adds Broker for in-memory usage.
type PeriodicTaskManagerOpts struct {
	PeriodicTaskConfigProvider asynq.PeriodicTaskConfigProvider
	RedisConnOpt               asynq.RedisConnOpt
	RedisUniversalClient       redis.UniversalClient
	SchedulerOpts              *asynq.SchedulerOpts
	SyncInterval               time.Duration

	// Broker is an asynqwrap extension used to back the periodic task manager
	// with either memory or redis through an existing broker instance.
	Broker Broker
}

// Inspector exposes queue and task inspection plus operational controls.
type Inspector interface {
	// Close releases inspector resources when the backend requires it.
	Close() error
	// Groups returns aggregation groups for the queue.
	Groups(queue string) ([]*asynq.GroupInfo, error)
	// GetQueueInfo returns queue-level counts, latency, and pause state.
	GetQueueInfo(queue string) (*asynq.QueueInfo, error)
	// GetTaskInfo returns metadata for a single task by queue and ID.
	GetTaskInfo(queue, id string) (*asynq.TaskInfo, error)
	// ListActiveTasks returns tasks currently being processed.
	ListActiveTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	// ListCompletedTasks returns tasks that are complete.
	ListCompletedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	// ListPendingTasks returns tasks waiting to be processed.
	ListPendingTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	// ListArchivedTasks returns tasks moved to archive/dead-letter state.
	ListArchivedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	// ListRetryTasks returns tasks waiting for another retry attempt.
	ListRetryTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	// ListScheduledTasks returns delayed tasks that have not been released yet.
	ListScheduledTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	// ArchiveTask moves a pending, scheduled, or retry task to archived state.
	ArchiveTask(queue, id string) error
	// DeleteTask removes a task that has not started processing.
	DeleteTask(queue, id string) error
	// DeleteAllArchivedTasks removes all archived tasks in the queue.
	DeleteAllArchivedTasks(queue string) (int, error)
	// DeleteAllRetryTasks removes all retry tasks in the queue.
	DeleteAllRetryTasks(queue string) (int, error)
	// PauseQueue stops workers from processing tasks from the queue.
	PauseQueue(queue string) error
	// Queues lists known queues.
	Queues() ([]string, error)
	// RunTask moves a scheduled, retry, or archived task back to pending state.
	RunTask(queue, id string) error
	// RunAllArchivedTasks moves all archived tasks in the queue back to pending state.
	RunAllArchivedTasks(queue string) (int, error)
	// RunAllRetryTasks moves all retry tasks in the queue back to pending state.
	RunAllRetryTasks(queue string) (int, error)
	// RunAllScheduledTasks moves all scheduled tasks in the queue back to pending state.
	RunAllScheduledTasks(queue string) (int, error)
	// UnpauseQueue resumes processing for the queue.
	UnpauseQueue(queue string) error
}

// Broker is the top-level entrypoint for creating clients, workers, schedulers, and inspectors.
type Broker interface {
	// Client returns an enqueue client for the configured backend.
	Client() Client
	// Server returns a worker server using the provided asynq configuration.
	Server(cfg asynq.Config) Server
	// Scheduler returns a recurring-task scheduler.
	Scheduler(opts *asynq.SchedulerOpts) Scheduler
	// PeriodicTaskManager returns a dynamic periodic-task manager backed by the broker.
	PeriodicTaskManager(opts PeriodicTaskManagerOpts) (PeriodicTaskManager, error)
	// Inspector returns an inspector for queue and task state.
	Inspector() Inspector
	// MetricsHandler returns an HTTP handler exposing Prometheus metrics.
	MetricsHandler() http.Handler
	// Close releases broker-owned resources.
	Close() error
}

// New creates a broker for the selected backend.
func New(cfg Config) (Broker, error) {
	switch cfg.Mode {
	case "", ModeRedis:
		redisKeyPrefix := prefixedStatePath(cfg.RuntimeConfig, "recurring-state")
		if cfg.RedisClient != nil {
			return newRedisBrokerFromClient(cfg.RedisClient, redisKeyPrefix), nil
		}
		if cfg.Redis == nil {
			return nil, oops.Errorf("redis mode requires Redis connection options")
		}
		return newRedisBroker(cfg.Redis, redisKeyPrefix), nil
	case ModeMemory:
		return newMemoryBroker(), nil
	case ModeSpfy:
		return newSpfyBroker(cfg)
	default:
		return nil, oops.Errorf("unsupported asynq mode %q", cfg.Mode)
	}
}

// NewFromRedisClient creates a Redis-backed broker from an existing go-redis client.
func NewFromRedisClient(client redis.UniversalClient, runtimeCfg config.Config) (Broker, error) {
	if client == nil {
		return nil, oops.Errorf("redis client is nil")
	}
	return newRedisBrokerFromClient(client, prefixedStatePath(runtimeCfg, "recurring-state")), nil
}

// NewPeriodicTaskManager creates a periodic task manager. It mirrors the
// upstream asynq constructor and additionally accepts Broker for memory-backed
// usage.
func NewPeriodicTaskManager(opts PeriodicTaskManagerOpts) (PeriodicTaskManager, error) {
	if opts.Broker != nil {
		return newPeriodicTaskManager(opts.Broker.Scheduler(opts.SchedulerOpts), opts)
	}
	if opts.RedisUniversalClient != nil || opts.RedisConnOpt != nil {
		upstream, err := asynq.NewPeriodicTaskManager(asynq.PeriodicTaskManagerOpts{
			PeriodicTaskConfigProvider: opts.PeriodicTaskConfigProvider,
			RedisConnOpt:               opts.RedisConnOpt,
			RedisUniversalClient:       opts.RedisUniversalClient,
			SchedulerOpts:              opts.SchedulerOpts,
			SyncInterval:               opts.SyncInterval,
		})
		if err != nil {
			return nil, err
		}
		return &redisPeriodicTaskManager{manager: upstream}, nil
	}
	return nil, oops.Errorf("PeriodicTaskManager requires Broker or RedisConnOpt/RedisUniversalClient")
}
