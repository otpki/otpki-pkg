package pulsarlog

import (
	"fmt"

	"github.com/apache/pulsar-client-go/pulsar/log"
	pkglog "github.com/otpki/otpki-pkg/log"
)

// PulsarLoggerAdapter adapts our Logger interface to the Pulsar logger interface
type PulsarLoggerAdapter struct {
	logger pkglog.Logger
}

// NewPulsarLogger creates a new Pulsar-compatible logger that wraps our Logger
// All logs will include a "component=pulsar" field for easy filtering
func NewPulsarLogger(logger pkglog.Logger) log.Logger {
	return &PulsarLoggerAdapter{
		logger: logger.With().Str("component", "pulsar").Logger(),
	}
}

// SubLogger returns a new logger with the provided fields
func (p *PulsarLoggerAdapter) SubLogger(fields log.Fields) log.Logger {
	ctx := p.logger.With()
	for key, value := range fields {
		ctx = ctx.Interface(key, value)
	}
	return &PulsarLoggerAdapter{
		logger: ctx.Logger(),
	}
}

// WithFields returns a new logger with the provided fields
func (p *PulsarLoggerAdapter) WithFields(fields log.Fields) log.Entry {
	ctx := p.logger.With()
	for key, value := range fields {
		ctx = ctx.Interface(key, value)
	}
	return &PulsarLogEntryAdapter{
		logger: ctx.Logger(),
	}
}

// WithField returns a new logger with the provided field
func (p *PulsarLoggerAdapter) WithField(name string, value any) log.Entry {
	return &PulsarLogEntryAdapter{
		logger: p.logger.With().Interface(name, value).Logger(),
	}
}

// WithError returns a new logger with an error field
func (p *PulsarLoggerAdapter) WithError(err error) log.Entry {
	return &PulsarLogEntryAdapter{
		logger: p.logger.With().Err(err).Logger(),
	}
}

// Debug logs a debug message
func (p *PulsarLoggerAdapter) Debug(_ ...any) {
	// p.logger.Trace().Msg(fmt.Sprint(args...))
}

// Info logs an info message
func (p *PulsarLoggerAdapter) Info(args ...any) {
	p.logger.Trace().Msg(fmt.Sprint(args...))
}

// Warn logs a warning message
func (p *PulsarLoggerAdapter) Warn(args ...any) {
	p.logger.Warn().Msg(fmt.Sprint(args...))
}

// Error logs an error message
func (p *PulsarLoggerAdapter) Error(args ...any) {
	p.logger.Error().Msg(fmt.Sprint(args...))
}

// Debugf logs a formatted debug message
func (p *PulsarLoggerAdapter) Debugf(_ string, _ ...any) {
	// p.logger.Trace().Msgf(format, args...)
}

// Infof logs a formatted info message
func (p *PulsarLoggerAdapter) Infof(format string, args ...any) {
	p.logger.Trace().Msgf(format, args...)
}

// Warnf logs a formatted warning message
func (p *PulsarLoggerAdapter) Warnf(format string, args ...any) {
	p.logger.Warn().Msgf(format, args...)
}

// Errorf logs a formatted error message
func (p *PulsarLoggerAdapter) Errorf(format string, args ...any) {
	p.logger.Error().Msgf(format, args...)
}

// PulsarLogEntryAdapter adapts our Logger to the Pulsar log.Entry interface
type PulsarLogEntryAdapter struct { //nolint:revive // Preserve the exported adapter name for existing callers.
	logger pkglog.Logger
}

// WithFields returns a new entry with the provided fields
func (e *PulsarLogEntryAdapter) WithFields(fields log.Fields) log.Entry {
	ctx := e.logger.With()
	for key, value := range fields {
		ctx = ctx.Interface(key, value)
	}
	return &PulsarLogEntryAdapter{
		logger: ctx.Logger(),
	}
}

// WithField returns a new entry with the provided field
func (e *PulsarLogEntryAdapter) WithField(name string, value any) log.Entry {
	return &PulsarLogEntryAdapter{
		logger: e.logger.With().Interface(name, value).Logger(),
	}
}

// Debug logs a debug message
func (e *PulsarLogEntryAdapter) Debug(_ ...any) {
	// e.logger.Trace().Msg(fmt.Sprint(args...))
}

// Info logs an info message
func (e *PulsarLogEntryAdapter) Info(args ...any) {
	e.logger.Trace().Msg(fmt.Sprint(args...))
}

// Warn logs a warning message
func (e *PulsarLogEntryAdapter) Warn(args ...any) {
	e.logger.Warn().Msg(fmt.Sprint(args...))
}

// Error logs an error message
func (e *PulsarLogEntryAdapter) Error(args ...any) {
	e.logger.Error().Msg(fmt.Sprint(args...))
}

// Debugf logs a formatted debug message
func (e *PulsarLogEntryAdapter) Debugf(_ string, _ ...any) {
	// e.logger.Trace().Msgf(format, args...)
}

// Infof logs a formatted info message
func (e *PulsarLogEntryAdapter) Infof(format string, args ...any) {
	e.logger.Trace().Msgf(format, args...)
}

// Warnf logs a formatted warning message
func (e *PulsarLogEntryAdapter) Warnf(format string, args ...any) {
	e.logger.Warn().Msgf(format, args...)
}

// Errorf logs a formatted error message
func (e *PulsarLogEntryAdapter) Errorf(format string, args ...any) {
	e.logger.Error().Msgf(format, args...)
}
