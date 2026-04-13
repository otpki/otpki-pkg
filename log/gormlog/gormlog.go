package gormlog

import (
	"context"
	"errors"
	"time"

	"github.com/otpki/otpki-pkg/log"
	"gorm.io/gorm/logger"
)

type GormLogger struct {
	log.Logger
}

func NewGormLogger(logger log.Logger) *GormLogger {
	if logger == nil {
		// default to global logger
		logger = log.GlobalLog
	}
	return &GormLogger{Logger: logger}
}

func (g *GormLogger) LogMode(_ logger.LogLevel) logger.Interface {
	return &GormLogger{}
}

func (g *GormLogger) Info(_ context.Context, s string, _ ...any) {
	g.Logger.Info().Msg(s)
}

func (g *GormLogger) Warn(_ context.Context, s string, _ ...any) {
	g.Logger.Warn().Msg(s)
}

func (g *GormLogger) Error(_ context.Context, s string, _ ...any) {
	g.Logger.Error().Msg(s)
}

func (g *GormLogger) Trace(_ context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()

	if err != nil && !errors.Is(err, logger.ErrRecordNotFound) {
		if rows == -1 {
			g.Logger.Err(err).Msgf("[%fms] - %s", float64(elapsed.Nanoseconds())/1e6, sql)
		} else {
			g.Logger.Err(err).Msgf("[%fms] [rows:%d] - %s", float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	} else {
		if rows == -1 {
			g.Logger.Trace().Msgf("[%fms] - %s", float64(elapsed.Nanoseconds())/1e6, sql)
		} else {
			g.Logger.Trace().Msgf("[%fms] [rows:%d] - %s", float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	}
}
