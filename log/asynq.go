package log

import (
	"fmt"

	"github.com/hibiken/asynq"
)

type AsynqLoggerAdapter struct {
	logger Logger
}

var _ asynq.Logger = (*AsynqLoggerAdapter)(nil)

func NewAsynqLogger(logger Logger) asynq.Logger {
	if logger == nil {
		logger = GlobalLog
	}
	logger = logger.With().Str("component", "tasks").Logger()
	return &AsynqLoggerAdapter{logger: logger}
}

func (l *AsynqLoggerAdapter) Debug(args ...any) { l.logger.Debug().Msg(fmt.Sprint(args...)) }
func (l *AsynqLoggerAdapter) Info(args ...any)  { l.logger.Info().Msg(fmt.Sprint(args...)) }
func (l *AsynqLoggerAdapter) Warn(args ...any)  { l.logger.Warn().Msg(fmt.Sprint(args...)) }
func (l *AsynqLoggerAdapter) Error(args ...any) { l.logger.Error().Msg(fmt.Sprint(args...)) }
func (l *AsynqLoggerAdapter) Fatal(args ...any) { l.logger.Fatal().Msg(fmt.Sprint(args...)) }
