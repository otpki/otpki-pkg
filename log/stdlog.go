package log

import (
	"io"
	stdlog "log"
)

// StdLogWriter is an adapter that allows using our logger with standard library
// interfaces that expect a *log.Logger
type StdLogWriter struct {
	level  Level
	logger Logger
}

func (w *StdLogWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	if n > 0 && p[n-1] == '\n' {
		// Trim trailing newline because our logger adds one
		p = p[0 : n-1]
	}
	w.logger.WithLevel(w.level).Msg(string(p))
	return n, nil
}

// NewStdLogWriter returns an io.Writer that writes to the logger at the specified level
func NewStdLogWriter(logger Logger, level Level) io.Writer {
	return &StdLogWriter{
		level:  level,
		logger: logger,
	}
}

// NewStdLogAdapter creates a standard library *log.Logger that writes to our logger
func NewStdLogAdapter(logger Logger, level Level) *stdlog.Logger {
	return stdlog.New(NewStdLogWriter(logger, level), "", 0)
}

// NewStdErrorLogAdapter is a convenience function specifically for HTTP Server ErrorLog
func NewStdErrorLogAdapter(logger Logger, component string) *stdlog.Logger {
	return NewStdLogAdapter(logger.With().Str("component", component).Logger(), ErrorLevel)
}
