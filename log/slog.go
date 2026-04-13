package log

import (
	"context"
	"log/slog"
)

// SlogHandler is a slog.Handler implementation that writes to our logger
type SlogHandler struct {
	logger Logger
}

func (h *SlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	loggerLevel := h.logger.GetLevel()
	switch level {
	case slog.LevelDebug:
		return loggerLevel <= DebugLevel
	case slog.LevelInfo:
		return loggerLevel <= InfoLevel
	case slog.LevelWarn:
		return loggerLevel <= WarnLevel
	case slog.LevelError:
		return loggerLevel <= ErrorLevel
	default:
		return true
	}
}

func (h *SlogHandler) Handle(_ context.Context, record slog.Record) error {
	// create a new event with the appropriate level
	var e Event
	switch record.Level {
	case slog.LevelDebug:
		e = h.logger.Debug()
	case slog.LevelInfo:
		e = h.logger.Info()
	case slog.LevelWarn:
		e = h.logger.Warn()
	case slog.LevelError:
		e = h.logger.Error()
	default:
		e = h.logger.Info()
	}

	// add attributes from the record
	record.Attrs(func(attr slog.Attr) bool {
		addAttrToEvent(e, attr)
		return true
	})

	e.Time("time", record.Time).Msg(record.Message)

	return nil
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	ctx := h.logger.With()
	for _, attr := range attrs {
		ctx = addAttrToContext(ctx, attr)
	}

	return &SlogHandler{logger: ctx.Logger()}
}

func (h *SlogHandler) WithGroup(name string) slog.Handler {
	// Create a sub-logger with the group name as a prefix
	return &SlogHandler{logger: h.logger.With().Str("group", name).Logger()}
}

// Helper function to add slog.Attr to Context
func addAttrToContext(ctx Context, attr slog.Attr) Context {
	switch attr.Value.Kind() {
	case slog.KindBool:
		return ctx.Bool(attr.Key, attr.Value.Bool())
	case slog.KindInt64:
		return ctx.Int64(attr.Key, attr.Value.Int64())
	case slog.KindString:
		return ctx.Str(attr.Key, attr.Value.String())
	case slog.KindTime:
		return ctx.Time(attr.Key, attr.Value.Time())
	case slog.KindGroup:
		// for groups, just add as a string representation since our logger
		// doesn't have a direct equivalent for nested contexts
		return ctx.Str(attr.Key, attr.Value.String())
	default:
		// for any other type, convert to string
		return ctx.Interface(attr.Key, attr.Value)
	}
}

// Helper function to add slog.Attr to Event
func addAttrToEvent(e Event, attr slog.Attr) {
	switch attr.Value.Kind() {
	case slog.KindBool:
		e.Bool(attr.Key, attr.Value.Bool())
	case slog.KindDuration:
		e.Dur(attr.Key, attr.Value.Duration())
	case slog.KindFloat64:
		e.Float64(attr.Key, attr.Value.Float64())
	case slog.KindInt64:
		e.Int64(attr.Key, attr.Value.Int64())
	case slog.KindString:
		e.Str(attr.Key, attr.Value.String())
	case slog.KindTime:
		e.Time(attr.Key, attr.Value.Time())
	case slog.KindUint64:
		e.Uint64(attr.Key, attr.Value.Uint64())
	case slog.KindGroup:
		// for groups, just add as a string representation
		e.Str(attr.Key, attr.Value.String())
	default:
		// for any other type, convert to string
		e.Interface(attr.Key, attr.Value)
	}
}

// NewSlogAdapter creates a slog.Logger that writes to our logger
func NewSlogAdapter(logger Logger, component string) *slog.Logger {
	handler := &SlogHandler{logger: logger.With().Str("component", component).Logger()}
	return slog.New(handler)
}
