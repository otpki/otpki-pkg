package log

import (
	"fmt"
	"io"

	daprlog "github.com/dapr/kit/logger"
)

// DaprLoggerAdapter adapts this package's Logger to Dapr's logger.Logger interface.
type DaprLoggerAdapter struct {
	logger Logger
	level  daprlog.LogLevel
}

var _ daprlog.Logger = (*DaprLoggerAdapter)(nil)

// NewDaprLoggerAdapter creates a Dapr-compatible logger backed by this package's Logger.
//
// The supplied scope is mapped to Dapr's "scope" field so logs remain filterable
// in the same way Dapr expects.
func NewDaprLoggerAdapter(logger Logger, scope string) daprlog.Logger {
	if logger == nil {
		logger = GlobalLog
	}

	adapter := &DaprLoggerAdapter{
		logger: logger,
		level:  daprlog.InfoLevel,
	}

	if scope != "" {
		adapter.logger = adapter.logger.With().Str("scope", scope).Logger()
	}

	return adapter
}

func (l *DaprLoggerAdapter) EnableJSONOutput(_ bool) {}

func (l *DaprLoggerAdapter) SetAppID(id string) {
	if l == nil || l.logger == nil {
		return
	}
	l.logger = l.logger.With().Str("app_id", id).Logger()
}

func (l *DaprLoggerAdapter) SetOutputLevel(outputLevel daprlog.LogLevel) {
	if l == nil {
		return
	}
	l.level = outputLevel
}

func (l *DaprLoggerAdapter) SetOutput(dst io.Writer) {
	if l == nil || l.logger == nil {
		return
	}

	type outputLogger interface {
		Output(io.Writer) Logger
	}

	if out, ok := l.logger.(outputLogger); ok {
		l.logger = out.Output(dst)
	}
}

func (l *DaprLoggerAdapter) IsOutputLevelEnabled(level daprlog.LogLevel) bool {
	if l == nil {
		return false
	}
	return daprLevelEnabled(l.level, level)
}

func (l *DaprLoggerAdapter) WithLogType(logType string) daprlog.Logger {
	cp := &DaprLoggerAdapter{level: daprlog.InfoLevel}
	if l != nil {
		cp.level = l.level
		cp.logger = l.logger
	}
	if cp.logger != nil {
		cp.logger = cp.logger.With().Str("type", logType).Logger()
	}
	return cp
}

func (l *DaprLoggerAdapter) WithFields(fields map[string]any) daprlog.Logger {
	cp := &DaprLoggerAdapter{level: daprlog.InfoLevel}
	if l != nil {
		cp.level = l.level
		cp.logger = l.logger
	}

	if len(fields) == 0 || cp.logger == nil {
		return cp
	}

	ctx := cp.logger.With()
	for key, value := range fields {
		ctx = ctx.Interface(key, value)
	}

	cp.logger = ctx.Logger()
	return cp
}

func (l *DaprLoggerAdapter) Info(args ...any) {
	l.log(daprlog.InfoLevel, l.logger.Debug, args...)
}

func (l *DaprLoggerAdapter) Infof(format string, args ...any) {
	l.logf(daprlog.InfoLevel, l.logger.Debug, format, args...)
}

func (l *DaprLoggerAdapter) Debug(args ...any) {
	l.log(daprlog.DebugLevel, l.logger.Trace, args...)
}

func (l *DaprLoggerAdapter) Debugf(format string, args ...any) {
	l.logf(daprlog.DebugLevel, l.logger.Trace, format, args...)
}

func (l *DaprLoggerAdapter) Warn(args ...any) {
	l.log(daprlog.WarnLevel, l.logger.Warn, args...)
}

func (l *DaprLoggerAdapter) Warnf(format string, args ...any) {
	l.logf(daprlog.WarnLevel, l.logger.Warn, format, args...)
}

func (l *DaprLoggerAdapter) Error(args ...any) {
	l.log(daprlog.ErrorLevel, l.logger.Error, args...)
}

func (l *DaprLoggerAdapter) Errorf(format string, args ...any) {
	l.logf(daprlog.ErrorLevel, l.logger.Error, format, args...)
}

func (l *DaprLoggerAdapter) Fatal(args ...any) {
	l.log(daprlog.FatalLevel, l.logger.Fatal, args...)
}

func (l *DaprLoggerAdapter) Fatalf(format string, args ...any) {
	l.logf(daprlog.FatalLevel, l.logger.Fatal, format, args...)
}

func (l *DaprLoggerAdapter) log(level daprlog.LogLevel, event func() Event, args ...any) {
	if l == nil || l.logger == nil || !l.IsOutputLevelEnabled(level) {
		return
	}
	event().Msg(fmt.Sprint(args...))
}

func (l *DaprLoggerAdapter) logf(level daprlog.LogLevel, event func() Event, format string, args ...any) {
	if l == nil || l.logger == nil || !l.IsOutputLevelEnabled(level) {
		return
	}
	event().Msgf(format, args...)
}

func daprLevelEnabled(current, requested daprlog.LogLevel) bool {
	return daprLevelRank(requested) >= daprLevelRank(current)
}

func daprLevelRank(level daprlog.LogLevel) int {
	switch level {
	case daprlog.DebugLevel:
		return 1
	case daprlog.InfoLevel:
		return 2
	case daprlog.WarnLevel:
		return 3
	case daprlog.ErrorLevel:
		return 4
	case daprlog.FatalLevel:
		return 5
	default:
		return 2
	}
}
