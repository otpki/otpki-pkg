package asynqwrap

import (
	"fmt"
	"log"
	"os"

	"github.com/hibiken/asynq"
)

type leveledLogger struct {
	logger asynq.Logger
	level  asynq.LogLevel
}

func newLeveledLogger(logger asynq.Logger, level asynq.LogLevel) *leveledLogger {
	if logger == nil {
		logger = stdlibLogger{logger: log.New(os.Stderr, "asynqwrap: ", log.LstdFlags)}
	}
	if level == 0 {
		level = asynq.InfoLevel
	}
	return &leveledLogger{logger: logger, level: level}
}

func (l *leveledLogger) Debugf(format string, args ...any) {
	if l.level <= asynq.DebugLevel {
		l.logger.Debug(fmt.Sprintf(format, args...))
	}
}

func (l *leveledLogger) Infof(format string, args ...any) {
	if l.level <= asynq.InfoLevel {
		l.logger.Info(fmt.Sprintf(format, args...))
	}
}

func (l *leveledLogger) Warnf(format string, args ...any) {
	if l.level <= asynq.WarnLevel {
		l.logger.Warn(fmt.Sprintf(format, args...))
	}
}

func (l *leveledLogger) Errorf(format string, args ...any) {
	if l.level <= asynq.ErrorLevel {
		l.logger.Error(fmt.Sprintf(format, args...))
	}
}

type stdlibLogger struct {
	logger *log.Logger
}

func (l stdlibLogger) Debug(args ...any) { l.logger.Print(args...) }
func (l stdlibLogger) Info(args ...any)  { l.logger.Print(args...) }
func (l stdlibLogger) Warn(args ...any)  { l.logger.Print(args...) }
func (l stdlibLogger) Error(args ...any) { l.logger.Print(args...) }
func (l stdlibLogger) Fatal(args ...any) { l.logger.Fatal(args...) }
