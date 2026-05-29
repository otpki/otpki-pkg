# asynqwrap

`asynqwrap` is a dual-backend task scheduler built around [`github.com/hibiken/asynq`](https://github.com/hibiken/asynq).

It gives you one API with two execution modes:

- `redis`: delegates to real `asynq` and Redis.
- `memory`: uses an in-process queue for environments that do not have Redis.

The Redis backend is the durable, production-grade backend. The memory backend is useful for single-process deployments, local development, tests, and installations where Redis is not available.

## Features

- At-least-once task execution
- Immediate and scheduled tasks
- Retries with backoff
- Panic recovery for worker handlers
- Weighted priority queues
- Strict priority queues
- Task de-duplication via `asynq.Unique`
- Per-task timeout and deadline
- Group aggregation via `asynq.GroupAggregator`
- Standard `asynq.Handler` and `asynq.ServeMux`
- Queue pause and unpause
- Periodic tasks
- Prometheus metrics
- Redis Sentinel / failover / cluster support through `asynq.RedisConnOpt`
- Existing `go-redis` client reuse

## Package Overview

Main interfaces:

- `Broker`
- `Client`
- `Server`
- `Scheduler`
- `PeriodicTaskManager`
- `Inspector`

Constructor:

```go
broker, err := asynqwrap.New(asynqwrap.Config{...})
```

## Choose a Backend

### Redis Backend

Use Redis when you want real `asynq` behavior with durability and multi-process recovery.

```go
broker, err := asynqwrap.New(asynqwrap.Config{
	Mode: asynqwrap.ModeRedis,
	Redis: asynq.RedisClientOpt{
		Addr: "127.0.0.1:6379",
	},
	RedisKeyPrefix: "tenant-a",
})
if err != nil {
	return err
}
defer broker.Close()
```

### Memory Backend

Use memory mode when Redis is not available.

```go
broker, err := asynqwrap.New(asynqwrap.Config{
	Mode: asynqwrap.ModeMemory,
})
if err != nil {
	return err
}
defer broker.Close()
```

## Reuse an Existing Redis Client

If your application already creates a `go-redis` client, pass it into the wrapper instead of rebuilding config.

```go
package main

import (
	"github.com/redis/go-redis/v9"

	"task/asynqwrap"
)

func example() error {
	rdb := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
	})

	broker, err := asynqwrap.New(asynqwrap.Config{
		Mode:           asynqwrap.ModeRedis,
		RedisClient:    rdb,
		RedisKeyPrefix: "tenant-a",
	})
	if err != nil {
		return err
	}
	defer broker.Close()

	_ = broker
	return nil
}
```

Or use the convenience constructor:

```go
broker, err := asynqwrap.NewFromRedisClient(rdb)
```

With a prefix for asynqwrap-owned Redis keys:

```go
broker, err := asynqwrap.NewFromRedisClientWithPrefix(rdb, "tenant-a")
```

`RedisKeyPrefix` only applies to internal keys owned by `asynqwrap`, such as recurring task metadata. It does not rename upstream `asynq` queue keys.

Internally, Redis mode uses:

- `asynq.NewClientFromRedisClient`
- `asynq.NewServerFromRedisClient`
- `asynq.NewSchedulerFromRedisClient`
- `asynq.NewPeriodicTaskManager`
- `asynq.NewInspectorFromRedisClient`

## Basic Usage

### 1. Create a Broker

```go
broker, err := asynqwrap.New(asynqwrap.Config{
	Mode: asynqwrap.ModeMemory,
})
if err != nil {
	return err
}
defer broker.Close()
```

### 2. Start a Worker Server

```go
mux := asynq.NewServeMux()

mux.HandleFunc("email:welcome", func(ctx context.Context, task *asynq.Task) error {
	log.Printf("sending welcome email: %s", task.Payload())
	return nil
})

server := broker.Server(asynq.Config{
	Concurrency: 10,
	Queues: map[string]int{
		"critical": 6,
		"default":  3,
		"low":      1,
	},
})

if err := server.Start(mux); err != nil {
	return err
}
defer server.Shutdown()
```

### 3. Enqueue a Task

```go
client := broker.Client()
defer client.Close()

task := asynq.NewTask("email:welcome", []byte(`{"user_id":42}`))

info, err := client.Enqueue(task, asynq.Queue("critical"))
if err != nil {
	return err
}

log.Printf("enqueued task id=%s queue=%s", info.ID, info.Queue)
```

## Time Helpers

The package includes small helpers for the common `time.Duration` and `time.Time` flows.

```go
info, err := asynqwrap.EnqueueNow(
	client,
	asynq.NewTask("job:run", nil),
)
```

```go
info, err := asynqwrap.EnqueueIn(
	client,
	15*time.Minute,
	asynq.NewTask("job:later", nil),
	asynq.Queue("default"),
)
```

```go
info, err := asynqwrap.EnqueueAt(
	client,
	time.Now().Add(2*time.Hour),
	asynq.NewTask("job:at", nil),
)
```

Recurring by interval:

```go
entryID, err := asynqwrap.RegisterEvery(
	scheduler,
	5*time.Minute,
	asynq.NewTask("reports:refresh", nil),
)
```

Build the `@every` spec without registering yet:

```go
spec, err := asynqwrap.CronSpecEvery(5 * time.Minute)
if err != nil {
	return err
}

entryID, err := scheduler.Register(spec, asynq.NewTask("reports:refresh", nil))
```

These helpers are wrappers around:

- `Client.Enqueue`
- `asynq.ProcessIn`
- `asynq.ProcessAt`
- scheduler `@every <duration>`

## Scheduling Tasks

Run a task in the future with normal `asynq` options:

```go
_, err := client.Enqueue(
	asynq.NewTask("report:generate", []byte(`{"account_id":10}`)),
	asynq.ProcessIn(15*time.Minute),
)
```

Or:

```go
_, err := client.Enqueue(
	asynq.NewTask("report:generate", []byte(`{"account_id":10}`)),
	asynq.ProcessAt(time.Now().Add(15*time.Minute)),
)
```

## Logger Configuration

Both backends accept the normal upstream `asynq` logger interfaces through
`asynq.Config` and `asynq.SchedulerOpts`.

Server logging:

```go
server := broker.Server(asynq.Config{
	Concurrency: 10,
	Logger:      myLogger,
	LogLevel:    asynq.InfoLevel,
})
```

Scheduler logging:

```go
scheduler := broker.Scheduler(&asynq.SchedulerOpts{
	Logger:   myLogger,
	LogLevel: asynq.InfoLevel,
})
```

This is useful if your application already provides its own structured logger,
or if you want to replace the previous "noop logger" setup with a real one.

## Retries

Set retries per task:

```go
_, err := client.Enqueue(
	asynq.NewTask("invoice:charge", payload),
	asynq.MaxRetry(10),
)
```

Override the retry delay strategy on the server:

```go
server := broker.Server(asynq.Config{
	Concurrency: 5,
	RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
		return 5 * time.Second
	},
})
```

Skip retry from a handler:

```go
return oops.Errorf("validation failed: %w", asynq.SkipRetry)
```

Revoke a task without retry/archive:

```go
return asynq.RevokeTask
```

## Inspecting Failures

There are two complementary ways to understand task failures:

1. real-time failure hooks through `ErrorHandler`
2. historical task inspection through `Inspector`

### Real-Time Failure Logging with `ErrorHandler`

Use `asynq.Config.ErrorHandler` when you want to log, alert, or report failures as soon as they happen.

```go
server := broker.Server(asynq.Config{
	Concurrency: 10,
	ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
		log.Printf("task failed type=%s err=%v", task.Type(), err)
	}),
})
```

This is the best place to:

- send errors to Sentry/Honeycomb/etc.
- increment custom failure metrics
- log payload metadata for debugging
- detect retry exhaustion

For example:

```go
ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)

	log.Printf(
		"task failed type=%s retried=%d max_retry=%d err=%v",
		task.Type(),
		retried,
		maxRetry,
		err,
	)
}),
```

### Historical Failure Inspection with `Inspector`

The wrapper also exposes failure-oriented inspector methods:

- `Groups(queue)`
- `GetTaskInfo(queue, id)`
- `ListActiveTasks(queue)`
- `ListPendingTasks(queue)`
- `ListScheduledTasks(queue)`
- `ListRetryTasks(queue)`
- `ListArchivedTasks(queue)`
- `ArchiveTask(queue, id)`
- `RunTask(queue, id)`
- `RunAllScheduledTasks(queue)`
- `RunAllRetryTasks(queue)`
- `RunAllArchivedTasks(queue)`
- `DeleteTask(queue, id)`
- `DeleteAllRetryTasks(queue)`
- `DeleteAllArchivedTasks(queue)`

Example:

```go
inspector := broker.Inspector()

retryTasks, err := inspector.ListRetryTasks("default")
if err != nil {
	return err
}

for _, task := range retryTasks {
	log.Printf(
		"retry task id=%s type=%s retried=%d max_retry=%d last_err=%q next=%s",
		task.ID,
		task.Type,
		task.Retried,
		task.MaxRetry,
		task.LastErr,
		task.NextProcessAt,
	)
}
```

Inspect archived tasks:

```go
archivedTasks, err := inspector.ListArchivedTasks("default")
if err != nil {
	return err
}

for _, task := range archivedTasks {
log.Printf(
	"archived task id=%s type=%s last_err=%q failed_at=%s",
		task.ID,
		task.Type,
		task.LastErr,
		task.LastFailedAt,
	)
}
```

Inspect a single task:

```go
taskInfo, err := inspector.GetTaskInfo("default", taskID)
if err != nil {
	return err
}

log.Printf(
	"task id=%s state=%s retried=%d last_err=%q",
	taskInfo.ID,
	taskInfo.State,
	taskInfo.Retried,
	taskInfo.LastErr,
)
```

Inspect pending or delayed work:

```go
pendingTasks, err := inspector.ListPendingTasks("default")
if err != nil {
	return err
}

scheduledTasks, err := inspector.ListScheduledTasks("default")
if err != nil {
	return err
}

activeTasks, err := inspector.ListActiveTasks("default")
if err != nil {
	return err
}
```

Inspect aggregation groups:

```go
groups, err := inspector.Groups("default")
if err != nil {
	return err
}

for _, group := range groups {
	log.Printf("group=%s size=%d", group.Group, group.Size)
}
```

Delete a task before it starts:

```go
if err := inspector.DeleteTask("default", taskID); err != nil {
	return err
}
```

Force a task to run now from scheduled/retry/archived state:

```go
if err := inspector.RunTask("default", taskID); err != nil {
	return err
}
```

Bulk operational controls:

```go
if _, err := inspector.RunAllScheduledTasks("default"); err != nil {
	return err
}

if _, err := inspector.RunAllRetryTasks("default"); err != nil {
	return err
}

if _, err := inspector.RunAllArchivedTasks("default"); err != nil {
	return err
}

if _, err := inspector.DeleteAllRetryTasks("default"); err != nil {
	return err
}

if _, err := inspector.DeleteAllArchivedTasks("default"); err != nil {
	return err
}
```

Archive a bad task without letting it continue to retry:

```go
if err := inspector.ArchiveTask("default", taskID); err != nil {
	return err
}
```

This works for tasks that have not started processing yet. If a worker already claimed the task, treat that as an active-processing problem rather than a pre-start deletion.

### Redis vs Memory Behavior

In `redis` mode, these inspector calls delegate to `asynq.Inspector`.

In `memory` mode, the wrapper exposes the same information from the in-process retry and archive state, including:

- `LastErr`
- `LastFailedAt`
- `Retried`
- `MaxRetry`
- `NextProcessAt`

### Asynq Web UI in Redis Mode

The upstream web UI comes from [`github.com/hibiken/asynqmon`](https://github.com/hibiken/asynqmon). It only works with the real Redis-backed backend, not the in-memory broker.

If you are using `asynqwrap.ModeRedis`, mount `asynqmon` with the same Redis connection settings you use for `asynqwrap`:

```go
package main

import (
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/hibiken/asynqmon"

	"task/asynqwrap"
)

func main() {
	redisOpt := asynq.RedisClientOpt{Addr: "127.0.0.1:6379"}

	broker, err := asynqwrap.New(asynqwrap.Config{
		Mode:  asynqwrap.ModeRedis,
		Redis: redisOpt,
	})
	if err != nil {
		panic(err)
	}
	defer broker.Close()

	http.Handle("/monitoring/tasks", asynqmon.New(asynqmon.Options{
		RootPath:     "/monitoring/tasks",
		RedisConnOpt: redisOpt,
	}))

	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
```

If your application builds the broker from an existing `go-redis` client, keep using that client for `asynqwrap`, and create the `asynqmon` handler with equivalent Redis connection settings. The monitoring UI is separate from `asynqwrap.Inspector`; `Inspector` stays available for programmatic access in both Redis and memory modes.

## Priority Queues

### Weighted Priority

```go
server := broker.Server(asynq.Config{
	Concurrency: 10,
	Queues: map[string]int{
		"critical": 6,
		"default":  3,
		"low":      1,
	},
})
```

This favors `critical` more often, while still allowing lower-priority queues to run.

### Strict Priority

```go
server := broker.Server(asynq.Config{
	Concurrency: 10,
	Queues: map[string]int{
		"critical": 100,
		"default":  10,
		"low":      1,
	},
	StrictPriority: true,
})
```

This drains higher-priority queues first.

## Unique Tasks

Use `asynq.Unique` to deduplicate tasks.

```go
_, err := client.Enqueue(
	asynq.NewTask("sync:user", []byte(`{"id":123}`)),
	asynq.Unique(10*time.Minute),
)
if errors.Is(err, asynq.ErrDuplicateTask) {
	log.Printf("task already exists")
}
```

Deduplication is based on:

- queue
- task type
- payload

## Timeout and Deadline

Per-task timeout:

```go
_, err := client.Enqueue(
	asynq.NewTask("video:transcode", payload),
	asynq.Timeout(30*time.Second),
)
```

Per-task deadline:

```go
_, err := client.Enqueue(
	asynq.NewTask("video:transcode", payload),
	asynq.Deadline(time.Now().Add(2*time.Minute)),
)
```

If both are set, the earlier limit wins.

## Group Aggregation

Aggregate bursts of related tasks into one batched task.

```go
server := broker.Server(asynq.Config{
	Concurrency:      10,
	GroupGracePeriod: 2 * time.Second,
	GroupMaxDelay:    10 * time.Second,
	GroupMaxSize:     100,
	GroupAggregator: asynq.GroupAggregatorFunc(func(group string, tasks []*asynq.Task) *asynq.Task {
		return asynq.NewTask("email:batch", []byte(fmt.Sprintf(`{"count":%d}`, len(tasks))))
	}),
})
```

Enqueue grouped tasks:

```go
_, err := client.Enqueue(
	asynq.NewTask("email:send", []byte(`{"user_id":1}`)),
	asynq.Group("welcome-email"),
)
```

In Redis mode, `asynq` handles grouping. In memory mode, grouping is implemented in-process using the same `asynq.GroupAggregator` interface.

## Middlewares and Handlers

Use `asynq.ServeMux` and middlewares normally.

```go
mux := asynq.NewServeMux()

mux.Use(func(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		err := next.ProcessTask(ctx, task)
		log.Printf("task=%s elapsed=%s err=%v", task.Type(), time.Since(start), err)
		return err
	})
})

mux.HandleFunc("order:submit", func(ctx context.Context, task *asynq.Task) error {
	return nil
})
```

Then:

```go
server := broker.Server(asynq.Config{Concurrency: 5})
if err := server.Start(mux); err != nil {
	return err
}
```

You can also register wrapper-level middleware directly on `Client` and `Server`:

```go
client := broker.Client()
client.Use(myEnqueueMiddleware)

server := broker.Server(asynq.Config{Concurrency: 5})
server.Use(myHandlerMiddleware)
```

`Client.Use` wraps `Enqueue` and `EnqueueContext`. `Server.Use` wraps the handler passed to `Start` or `Run`.

This is one place where `asynqwrap` intentionally extends upstream `asynq`: `asynq.Client` and `asynq.Server` do not have `Use` methods. The task model, handler signatures, `asynq.Config`, `asynq.Option`, `asynq.SchedulerOpts`, and `asynq.ServeMux` usage still stay aligned with upstream.

## Tracing

Use OpenTelemetry spans around both enqueue and handler execution by combining:

- `Client.Use(asynqwrap.TraceEnqueueMiddleware(...))`
- `Server.Use(asynqwrap.TraceHandlerMiddleware(...))`
- task headers for context propagation

Example:

```go
package main

import (
	"context"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"task/asynqwrap"
)

func configureTracing(broker asynqwrap.Broker, tracer trace.Tracer) error {
	traceConfig := asynqwrap.TraceConfig{
		Tracer:     tracer,
		Propagator: propagation.TraceContext{},
	}

	client := broker.Client()
	client.Use(asynqwrap.TraceEnqueueMiddleware(traceConfig))

	server := broker.Server(asynq.Config{Concurrency: 10})
	server.Use(asynqwrap.TraceHandlerMiddleware(traceConfig))

	mux := asynq.NewServeMux()
	mux.HandleFunc("email:welcome", func(ctx context.Context, task *asynq.Task) error {
		// SpanFromContext(ctx) is now the consumer span for this task.
		return nil
	})

	if err := server.Start(mux); err != nil {
		return err
	}

	_, err := client.EnqueueContext(ctx, asynq.NewTask("email:welcome", nil))
	return err
}
```

`TraceEnqueueMiddleware` starts a producer span and injects the current trace context into task headers. `TraceHandlerMiddleware` extracts that context and starts a consumer span around `ProcessTask`.

### Propagating an Existing Trace

If you already have an active span, pass that context into `EnqueueContext`.

That is the important part: the existing trace is propagated from the `ctx` you give to `EnqueueContext`, not from the task payload.

Flow:

1. your HTTP handler, RPC handler, or parent job has a `ctx` with an active span
2. `TraceEnqueueMiddleware` starts a producer span as a child of that active span
3. the middleware injects trace context into task headers
4. `TraceHandlerMiddleware` extracts those headers on the worker side
5. the worker starts a consumer span as a child of the producer span

Example from an HTTP request:

```go
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // already carries the request span

	task := asynq.NewTask("order:submit", []byte(`{"order_id":42}`))
	if _, err := h.tasks.EnqueueContext(ctx, task); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
```

On the worker side:

```go
server := broker.Server(asynq.Config{Concurrency: 10})
server.Use(asynqwrap.TraceHandlerMiddleware(traceConfig))

mux := asynq.NewServeMux()
mux.HandleFunc("order:submit", func(ctx context.Context, task *asynq.Task) error {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return oops.New("missing propagated trace context")
	}
	return nil
})
```

If you call `Enqueue` instead of `EnqueueContext`, the wrapper uses `context.Background()`. That still creates a producer span, but it will not be attached to an existing request trace.

### What Gets Propagated

Propagation uses task headers, not the payload. With the default `propagation.TraceContext{}` propagator, the headers include standard W3C trace fields such as `traceparent` and, when present, `tracestate`.

That means:

- task payload stays focused on business data
- tracing works the same in Redis and memory mode
- inspectors can still show task payload without mixing in observability metadata

If you want baggage or a composite propagator, provide it through `TraceConfig.Propagator`.

```go
traceConfig := asynqwrap.TraceConfig{
	Tracer: tracer,
	Propagator: propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	),
}
```

### Producer and Consumer Semantics

The tracing helpers intentionally create two spans:

- producer span at enqueue time
- consumer span at task execution time

That matches async messaging semantics better than trying to reuse the same span across process boundaries. The consumer span is linked by parent-child propagation through task headers.

In practice, the trace shape is:

1. request span
2. enqueue span
3. process span

If enqueue fails, the producer span is marked as an error. If the handler returns an error, the consumer span is marked as an error.

### Tracing Periodic Tasks

Recurring jobs do not have an incoming request context, so use `WithTracingSchedulerOpts` to create a producer span each time the scheduler enqueues a task.

```go
traceConfig := asynqwrap.TraceConfig{
	Tracer:     tracer,
	Propagator: propagation.TraceContext{},
}

scheduler := broker.Scheduler(asynqwrap.WithTracingSchedulerOpts(nil, traceConfig))
defer scheduler.Shutdown()

_, err := scheduler.Register("@every 1m", asynq.NewTask("metrics:flush", nil))
if err != nil {
	return err
}

if err := scheduler.Start(); err != nil {
	return err
}
```

That gives you a scheduler-created producer span and a worker-created consumer span for each run.

If you need recurring jobs to attach to some external parent context, upstream `asynq.SchedulerOpts` does not provide a request-scoped context hook for each enqueue. In that case you would need to create your own scheduling layer on top and call `Client.EnqueueContext` directly with the parent context you want.

## Dependency Injection

If your handlers need services beyond what is stored in the task payload, resolve those dependencies from your application container and close over them in the handler.

With [`samber/do`](https://github.com/samber/do), the usual pattern is:

1. build the injector once at startup
2. resolve the services your handlers need
3. register handlers that use those services

Example:

```go
package main

import (
	"context"
	"encoding/json"

	"github.com/hibiken/asynq"
	"github.com/samber/do"
)

type EmailSender interface {
	SendWelcome(ctx context.Context, userID int64) error
}

type WelcomePayload struct {
	UserID int64 `json:"user_id"`
}

type WelcomeHandler struct {
	email EmailSender
}

func NewWelcomeHandler(i *do.Injector) (*WelcomeHandler, error) {
	email := do.MustInvoke[EmailSender](i)
	return &WelcomeHandler{email: email}, nil
}

func (h *WelcomeHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload WelcomePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	return h.email.SendWelcome(ctx, payload.UserID)
}

func registerHandlers(mux *asynq.ServeMux, i *do.Injector) error {
	welcomeHandler, err := NewWelcomeHandler(i)
	if err != nil {
		return err
	}

	mux.Handle("email:welcome", welcomeHandler)
	return nil
}
```

Using it:

```go
injector := do.New()

do.Provide(injector, func(i *do.Injector) (EmailSender, error) {
	return &MyEmailService{}, nil
})

mux := asynq.NewServeMux()
if err := registerHandlers(mux, injector); err != nil {
	return err
}

server := broker.Server(asynq.Config{Concurrency: 10})
if err := server.Start(mux); err != nil {
	return err
}
```

This is usually the cleanest approach because:

- the injector is used only at startup
- handlers keep explicit dependencies
- task payload stays focused on task data

### Using `BaseContext` with `do`

If you want the injector available from task context, attach it through `BaseContext`.

```go
type injectorKey struct{}

server := broker.Server(asynq.Config{
	BaseContext: func() context.Context {
		return context.WithValue(context.Background(), injectorKey{}, injector)
	},
})

mux := asynq.NewServeMux()
mux.HandleFunc("email:welcome", func(ctx context.Context, task *asynq.Task) error {
	i := ctx.Value(injectorKey{}).(*do.Injector)
	email := do.MustInvoke[EmailSender](i)

	var payload WelcomePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	return email.SendWelcome(ctx, payload.UserID)
})
```

This works, but constructor-style injection is usually easier to test and reason about. Prefer `BaseContext` when you have a strong reason to access request/task-scoped context values from handlers.

## Pause and Unpause a Queue

```go
inspector := broker.Inspector()

if err := inspector.PauseQueue("critical"); err != nil {
	return err
}

if err := inspector.UnpauseQueue("critical"); err != nil {
	return err
}
```

Get queue metadata:

```go
info, err := inspector.GetQueueInfo("critical")
if err != nil {
	return err
}

log.Printf(
	"queue=%s pending=%d active=%d scheduled=%d retry=%d paused=%v",
	info.Queue,
	info.Pending,
	info.Active,
	info.Scheduled,
	info.Retry,
	info.Paused,
)
```

## Periodic Tasks

Create a scheduler:

```go
scheduler := broker.Scheduler(nil)
defer scheduler.Shutdown()

_, err := scheduler.Register("@every 1m", asynq.NewTask("metrics:flush", nil))
if err != nil {
	return err
}

if err := scheduler.Start(); err != nil {
	return err
}
```

You can also use cron expressions:

```go
_, err := scheduler.Register("0 * * * *", asynq.NewTask("hourly:rollup", nil))
```

### Dynamic Periodic Tasks

If periodic tasks come from configuration or a database, use
`PeriodicTaskManager` instead of registering everything once at startup.

Redis-backed construction, matching upstream `asynq`:

```go
mgr, err := asynqwrap.NewPeriodicTaskManager(asynqwrap.PeriodicTaskManagerOpts{
	RedisConnOpt: asynq.RedisClientOpt{
		Addr: "127.0.0.1:6379",
	},
	PeriodicTaskConfigProvider: provider,
	SchedulerOpts: &asynq.SchedulerOpts{
		Logger:   myLogger,
		LogLevel: asynq.InfoLevel,
	},
	SyncInterval: time.Minute,
})
if err != nil {
	return err
}
defer mgr.Shutdown()

if err := mgr.Start(); err != nil {
	return err
}
```

If you already have a broker, use the broker convenience method instead:

```go
mgr, err := broker.PeriodicTaskManager(asynqwrap.PeriodicTaskManagerOpts{
	PeriodicTaskConfigProvider: provider,
	SchedulerOpts: &asynq.SchedulerOpts{
		Logger:   myLogger,
		LogLevel: asynq.InfoLevel,
	},
	SyncInterval: time.Minute,
})
if err != nil {
	return err
}
defer mgr.Shutdown()

if err := mgr.Start(); err != nil {
	return err
}
```

For memory mode, `broker.PeriodicTaskManager(...)` is the recommended entrypoint
because it binds the manager to the in-process backend automatically.

Example provider:

```go
type StaticProvider struct {
	configs []*asynq.PeriodicTaskConfig
}

func (p *StaticProvider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	return p.configs, nil
}

provider := &StaticProvider{
	configs: []*asynq.PeriodicTaskConfig{
		{
			Cronspec: "@every 1m",
			Task:     asynq.NewTask("metrics:flush", nil),
			Opts:     []asynq.Option{asynq.Queue("default")},
		},
		{
			Cronspec: "0 * * * *",
			Task:     asynq.NewTask("hourly:rollup", nil),
		},
	},
}
```

### Self-Rescheduling Recurring Tasks Without a Scheduler

If you want one task to reschedule itself after each successful run, use the recurring task helpers instead of `Scheduler` or `PeriodicTaskManager`.

This pattern is useful when:

- an API call upserts the task configuration
- you want the next run to be stored as a normal delayed task
- multiple servers may enqueue or process the task
- you do not want a singleton scheduler process or database polling loop

The helpers are:

- `UpsertRecurringTask`
- `DeleteRecurringTask`
- `SelfReschedulingTaskMiddleware`

`UpsertRecurringTask` deletes pending, scheduled, and retry copies for the same logical task key and updates internal recurring state. The first insert enqueues the task immediately. Later upserts schedule the next run for now + interval. `SelfReschedulingTaskMiddleware` is generic for all tasks and re-enqueues any task that was created through `UpsertRecurringTask`, using metadata stored on the task itself.

Example:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"task/asynqwrap"
)

type ReminderConfig struct {
	Key      string
	UserID   int64
	Interval time.Duration
}

func wireRecurringTask(broker asynqwrap.Broker) error {
	client := broker.Client()
	inspector := broker.Inspector()

	server := broker.Server(asynq.Config{Concurrency: 10})
	server.Use(asynqwrap.SelfReschedulingTaskMiddleware(client, inspector))

	mux := asynq.NewServeMux()
	mux.HandleFunc("reminder:send", func(ctx context.Context, task *asynq.Task) error {
		_ = task.Payload() // do the real work here with your normal task payload
		return nil
	})

	return server.Start(mux)
}
```

Upsert from an API call:

```go
func upsertReminder(ctx context.Context, broker asynqwrap.Broker, cfg ReminderConfig) error {
	_, err := asynqwrap.UpsertRecurringTask(
		ctx,
		broker.Client(),
		broker.Inspector(),
		asynqwrap.RecurringTaskSpec[string]{
			Key:      cfg.Key,
			Task:     asynq.NewTask("reminder:send", []byte(fmt.Sprintf(`{"user_id":%d}`, cfg.UserID))),
			Interval: cfg.Interval,
			Queue:    "default",
		},
	)
	return err
}
```

Delete the task entirely:

```go
_, err := asynqwrap.DeleteRecurringTask(
	ctx,
	broker.Inspector(),
	"reminder:user:42",
)
```

Operational notes:

- The recurring helpers work in both `redis` and `memory` modes because they only depend on `Client` and `Inspector`.
- `RecurringTaskSpec.Task` is a normal `*asynq.Task`; the wrapper stores recurring metadata in internal headers, not in your payload.
- The first `UpsertRecurringTask` for a key runs immediately. Later upserts schedule the next run for now + interval.
- Rescheduling happens after a successful handler call. Failures still use normal `asynq` retry behavior.
- If a task is upserted or deleted while an older run is still active, the older run will not enqueue another copy after it finishes.
- In Redis mode, active-task invalidation uses internal recurring state stored in Redis, so all instances see the same latest version for a recurring key.

## Prometheus Metrics

Expose the metrics handler:

```go
http.Handle("/metrics", broker.MetricsHandler())
log.Fatal(http.ListenAndServe(":2112", nil))
```

Current metrics include queue state counts, queue latency, pause state, processed totals, and failed totals for both backends.

## Redis Sentinel / Failover

Redis mode accepts any `asynq.RedisConnOpt`, so Sentinel and failover work through normal `asynq` options.

Example:

```go
broker, err := asynqwrap.New(asynqwrap.Config{
	Mode: asynqwrap.ModeRedis,
	Redis: asynq.RedisFailoverClientOpt{
		MasterName:    "mymaster",
		SentinelAddrs: []string{"127.0.0.1:26379", "127.0.0.1:26380"},
	},
})
```

## Backend Differences

### Redis Mode

- Uses real `asynq`
- Durable across process restarts
- Supports multi-process workers
- Suitable for production

### Memory Mode

- Single-process only
- No external Redis dependency
- Recovers handler panics and retries failed tasks
- Loses queued state if the process exits

This means memory mode is at-least-once only for the lifetime of the current process. If the process crashes or is terminated, in-memory state is lost.

## End-to-End Example

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/hibiken/asynq"

	"task/asynqwrap"
)

func main() {
	broker, err := asynqwrap.New(asynqwrap.Config{
		Mode: asynqwrap.ModeMemory,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer broker.Close()

	mux := asynq.NewServeMux()
	mux.HandleFunc("email:welcome", func(ctx context.Context, task *asynq.Task) error {
		log.Printf("processed task type=%s payload=%s", task.Type(), task.Payload())
		return nil
	})

	server := broker.Server(asynq.Config{
		Concurrency: 4,
		Queues: map[string]int{
			"critical": 3,
			"default":  1,
		},
	})
	if err := server.Start(mux); err != nil {
		log.Fatal(err)
	}
	defer server.Shutdown()

	client := broker.Client()
	defer client.Close()

	_, err = client.Enqueue(
		asynq.NewTask("email:welcome", []byte(`{"user_id":42}`)),
		asynq.Queue("critical"),
		asynq.MaxRetry(5),
		asynq.Timeout(10*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	http.Handle("/metrics", broker.MetricsHandler())
	go func() {
		log.Fatal(http.ListenAndServe(":2112", nil))
	}()

	select {}
}
```

## Validation

The package test suite covers:

- immediate execution
- delayed scheduling
- retries
- pause/unpause
- uniqueness
- weighted priority
- aggregation
- periodic scheduling
- metrics exposure

Run:

```bash
go test ./...
```
