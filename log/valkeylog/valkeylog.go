package valkeylog

import (
	"context"

	pkglog "github.com/otpki/otpki-pkg/log"
)

type ValkeyLoggerAdapter struct {
	logger pkglog.Logger
	level  pkglog.Level
}

func NewValkeyLogger(logger pkglog.Logger, level pkglog.Level) *ValkeyLoggerAdapter {
	return &ValkeyLoggerAdapter{
		logger: logger.With().Str("component", "redis").Logger(),
		level:  level,
	}
}

func (r *ValkeyLoggerAdapter) Printf(ctx context.Context, format string, v ...any) {
	r.logger.WithLevel(r.level).Ctx(ctx).Msgf(format, v...)
}
