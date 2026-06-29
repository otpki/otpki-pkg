package asynqwrap

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

type taskResultWriterKey struct{}

type taskResultBackend interface {
	WriteTaskResult(ctx context.Context, data []byte) (int, error)
}

type taskResultBinding struct {
	id      string
	qname   string
	backend taskResultBackend
}

func withTaskResultWriter(ctx context.Context, id, qname string, backend taskResultBackend) context.Context {
	if backend == nil {
		return ctx
	}
	return context.WithValue(ctx, taskResultWriterKey{}, taskResultBinding{
		id:      id,
		qname:   qname,
		backend: backend,
	})
}

// ResultWriter is a client interface to write result data for a task.
type ResultWriter struct {
	id       string
	qname    string
	backend  taskResultBackend
	upstream *asynq.ResultWriter
	ctx      context.Context
}

// Write writes the given data as a result of the task the ResultWriter is associated with.
func (w *ResultWriter) Write(data []byte) (n int, err error) {
	if w == nil {
		return 0, oops.New("asynq task result writer is unavailable")
	}
	if w.ctx == nil {
		return 0, oops.New("asynq task result writer context is unavailable")
	}
	select {
	case <-w.ctx.Done():
		return 0, oops.Errorf("failed to write task result: %w", w.ctx.Err())
	default:
	}
	if w.upstream != nil {
		return w.upstream.Write(data)
	}
	if w.backend == nil {
		return 0, oops.New("asynq task result writer backend is unavailable")
	}
	return w.backend.WriteTaskResult(w.ctx, data)
}

// TaskID returns the ID of the task the ResultWriter is associated with.
func (w *ResultWriter) TaskID() string {
	if w == nil {
		return ""
	}
	return w.id
}

// TaskResultWriter returns the result writer for a task.
//
// For the native Redis-backed asynq server, this delegates to task.ResultWriter().
// For asynqwrap's memory and spfy backends, asynq's internal ResultWriter cannot
// be constructed outside the asynq module, so this returns a context-backed
// writer installed while the handler is running.
func TaskResultWriter(ctx context.Context, task *asynq.Task) *ResultWriter {
	if task != nil {
		if writer := task.ResultWriter(); writer != nil {
			return &ResultWriter{
				id:       writer.TaskID(),
				upstream: writer,
				ctx:      ctx,
			}
		}
	}
	if binding, ok := ctx.Value(taskResultWriterKey{}).(taskResultBinding); ok {
		return &ResultWriter{
			id:      binding.id,
			qname:   binding.qname,
			backend: binding.backend,
			ctx:     ctx,
		}
	}
	return nil
}

// WriteTaskResult writes task result data using the available result writer.
func WriteTaskResult(ctx context.Context, task *asynq.Task, data []byte) (int, error) {
	writer := TaskResultWriter(ctx, task)
	if writer == nil {
		return 0, oops.New("asynq task result writer is unavailable")
	}
	return writer.Write(data)
}
