package asynqwrap

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const schedulerTraceAttemptHeader = "asynqwrap-trace-attempt"

// TraceConfig controls how asynqwrap creates spans and propagates task headers.
//
// Zero values fall back to the global OpenTelemetry tracer and propagator.
// Name functions are optional; defaults are derived from the task type.
type TraceConfig struct {
	Tracer trace.Tracer

	// Propagator injects and extracts trace context through task headers.
	Propagator propagation.TextMapPropagator

	// ProducerSpanName formats the span name used when a task is enqueued.
	ProducerSpanName func(task *asynq.Task) string

	// ConsumerSpanName formats the span name used when a task starts processing.
	ConsumerSpanName func(task *asynq.Task) string

	// ScheduledSpanName formats the span name used when a scheduler enqueues a recurring task.
	ScheduledSpanName func(task *asynq.Task) string
}

// TraceEnqueueMiddleware returns enqueue middleware that starts a producer span
// and injects the current trace context into task headers.
func TraceEnqueueMiddleware(cfg TraceConfig) EnqueueMiddleware {
	cfg = cfg.withDefaults()

	return func(next EnqueueFunc) EnqueueFunc {
		return func(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
			ctx, span := cfg.Tracer.Start(ctx, cfg.ProducerSpanName(task), trace.WithSpanKind(trace.SpanKindProducer))
			defer span.End()

			cfg.Propagator.Inject(ctx, headerCarrier(ensureTaskHeaders(task)))
			span.SetAttributes(attribute.String("asynq.task.type", task.Type()))

			info, err := next(ctx, task, opts...)
			recordEnqueueResult(span, info, err)
			return info, err
		}
	}
}

// TraceHandlerMiddleware returns handler middleware that extracts trace context
// from task headers and starts a consumer span for the task execution.
func TraceHandlerMiddleware(cfg TraceConfig) asynq.MiddlewareFunc {
	cfg = cfg.withDefaults()

	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			ctx = cfg.Propagator.Extract(ctx, headerCarrier(task.Headers()))
			ctx, span := cfg.Tracer.Start(ctx, cfg.ConsumerSpanName(task), trace.WithSpanKind(trace.SpanKindConsumer))
			defer span.End()

			span.SetAttributes(attribute.String("asynq.task.type", task.Type()))

			err := next.ProcessTask(ctx, task)
			recordTaskResult(span, err)
			return err
		})
	}
}

// WithTracingSchedulerOpts returns SchedulerOpts wired to start producer spans
// and inject trace context whenever the scheduler enqueues a recurring task.
//
// The returned struct preserves existing callbacks and invokes them after the
// tracing hooks run.
func WithTracingSchedulerOpts(opts *asynq.SchedulerOpts, cfg TraceConfig) *asynq.SchedulerOpts {
	cfg = cfg.withDefaults()
	if opts == nil {
		opts = &asynq.SchedulerOpts{}
	}

	out := *opts
	state := &schedulerTraceState{
		cfg:  cfg,
		pre:  opts.PreEnqueueFunc,
		post: opts.PostEnqueueFunc,
	}

	out.PreEnqueueFunc = state.preEnqueue
	out.PostEnqueueFunc = state.postEnqueue
	return &out
}

type schedulerTraceState struct {
	cfg  TraceConfig
	pre  func(task *asynq.Task, opts []asynq.Option)
	post func(info *asynq.TaskInfo, err error)

	mu           sync.Mutex
	spans        map[string]trace.Span
	attemptOrder []string
}

func (s *schedulerTraceState) preEnqueue(task *asynq.Task, opts []asynq.Option) {
	ctx, span := s.cfg.Tracer.Start(
		context.Background(),
		s.cfg.ScheduledSpanName(task),
		trace.WithSpanKind(trace.SpanKindProducer),
	)

	headers := ensureTaskHeaders(task)
	attemptID := uuid.NewString()
	headers[schedulerTraceAttemptHeader] = attemptID
	s.cfg.Propagator.Inject(ctx, headerCarrier(headers))

	s.mu.Lock()
	if s.spans == nil {
		s.spans = make(map[string]trace.Span)
	}
	s.spans[attemptID] = span
	s.attemptOrder = append(s.attemptOrder, attemptID)
	s.mu.Unlock()

	span.SetAttributes(attribute.String("asynq.task.type", task.Type()))

	if s.pre != nil {
		s.pre(task, opts)
	}
}

func (s *schedulerTraceState) postEnqueue(info *asynq.TaskInfo, err error) {
	if span := s.spanForEnqueue(info); span != nil {
		recordEnqueueResult(span, info, err)
		span.End()
	}

	if s.post != nil {
		s.post(info, err)
	}
}

func (s *schedulerTraceState) takeSpan(attemptID string) trace.Span {
	if attemptID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	span := s.spans[attemptID]
	delete(s.spans, attemptID)
	return span
}

func (s *schedulerTraceState) spanForEnqueue(info *asynq.TaskInfo) trace.Span {
	if info != nil {
		if span := s.takeSpan(info.Headers[schedulerTraceAttemptHeader]); span != nil {
			return span
		}
	}
	return s.takeNextSpan()
}

func (s *schedulerTraceState) takeNextSpan() trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.attemptOrder) == 0 {
		return nil
	}
	attemptID := s.attemptOrder[0]
	s.attemptOrder = s.attemptOrder[1:]
	span := s.spans[attemptID]
	delete(s.spans, attemptID)
	return span
}

type headerCarrier map[string]string

func (c headerCarrier) Get(key string) string { return c[key] }

func (c headerCarrier) Set(key, value string) { c[key] = value }

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

func (c TraceConfig) withDefaults() TraceConfig {
	if c.Tracer == nil {
		c.Tracer = otel.Tracer("asynqwrap")
	}
	if c.Propagator == nil {
		c.Propagator = otel.GetTextMapPropagator()
	}
	if c.ProducerSpanName == nil {
		c.ProducerSpanName = func(task *asynq.Task) string {
			return "asynq enqueue " + task.Type()
		}
	}
	if c.ConsumerSpanName == nil {
		c.ConsumerSpanName = func(task *asynq.Task) string {
			return "asynq process " + task.Type()
		}
	}
	if c.ScheduledSpanName == nil {
		c.ScheduledSpanName = func(task *asynq.Task) string {
			return "asynq schedule " + task.Type()
		}
	}
	return c
}

func recordEnqueueResult(span trace.Span, info *asynq.TaskInfo, err error) {
	if info != nil {
		span.SetAttributes(
			attribute.String("asynq.task.id", info.ID),
			attribute.String("asynq.queue", info.Queue),
		)
	}
	recordTaskResult(span, err)
}

func recordTaskResult(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
