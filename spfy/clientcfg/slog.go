package clientcfg

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/IBM/sarama"
	"github.com/aws/smithy-go/logging"
	"github.com/jackc/pgx/v5/tracelog"
)

type saramaSlogLogger struct{ logger *slog.Logger }

func (l *saramaSlogLogger) Print(v ...interface{}) { l.logger.Info(fmt.Sprint(v...)) }
func (l *saramaSlogLogger) Printf(format string, v ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, v...))
}
func (l *saramaSlogLogger) Println(v ...interface{}) {
	l.logger.Info(strings.TrimSuffix(fmt.Sprintln(v...), "\n"))
}

func NewSaramaLogger(logger *slog.Logger) sarama.StdLogger {
	if logger == nil {
		return nil
	}
	return &saramaSlogLogger{logger: logger}
}

type redisSlogLogger struct{ logger *slog.Logger }

func (l *redisSlogLogger) Printf(ctx context.Context, format string, v ...interface{}) {
	l.logger.InfoContext(ctx, fmt.Sprintf(format, v...))
}

func NewRedisLogger(logger *slog.Logger) interface {
	Printf(context.Context, string, ...interface{})
} {
	if logger == nil {
		return nil
	}
	return &redisSlogLogger{logger: logger}
}

type smithySlogLogger struct{ logger *slog.Logger }

func (l *smithySlogLogger) Logf(classification logging.Classification, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	switch classification {
	case logging.Warn:
		l.logger.Warn(msg)
	case logging.Debug:
		l.logger.Debug(msg)
	default:
		l.logger.Info(msg)
	}
}

func NewSmithyLogger(logger *slog.Logger) logging.Logger {
	if logger == nil {
		return nil
	}
	return &smithySlogLogger{logger: logger}
}

type pgxSlogLogger struct{ logger *slog.Logger }

func (l *pgxSlogLogger) Log(ctx context.Context, level tracelog.LogLevel, msg string, data map[string]any) {
	args := make([]any, 0, len(data)*2)
	for k, v := range data {
		args = append(args, k, v)
	}
	switch level {
	case tracelog.LogLevelTrace, tracelog.LogLevelDebug:
		l.logger.DebugContext(ctx, msg, args...)
	case tracelog.LogLevelInfo:
		l.logger.InfoContext(ctx, msg, args...)
	case tracelog.LogLevelWarn:
		l.logger.WarnContext(ctx, msg, args...)
	case tracelog.LogLevelError:
		l.logger.ErrorContext(ctx, msg, args...)
	default:
		l.logger.InfoContext(ctx, msg, args...)
	}
}

func NewPGXLogger(logger *slog.Logger) tracelog.Logger {
	if logger == nil {
		return nil
	}
	return &pgxSlogLogger{logger: logger}
}
