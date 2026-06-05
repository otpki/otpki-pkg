package log

import (
	"bytes"
	"encoding/json"
	"io"
	stdlog "log"
	"strings"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/rs/zerolog"
	otellog "go.opentelemetry.io/otel/log"
)

const (
	hclogRawJSONField = "zerolog_raw_json"
	hclogLoggerField  = "logger"
)

// NewHCLogLogger adapts your existing Logger interface to hclog.Logger.
//
// Use this wherever HashiCorp go-plugin requires hclog.Logger:
//   - host:   plugin.ClientConfig.Logger
//   - plugin: plugin.ServeConfig.Logger
func NewHCLogLogger(base Logger, level hclog.Level) hclog.Logger {
	if level == hclog.NoLevel {
		level = hclog.Trace
	}
	return &hcLogToLogger{
		base:  base,
		level: level,
	}
}

// NewPluginTransportLogger creates your normal Logger interface for plugin
// application code, but sends zerolog JSON events into the hclog transport.
//
// Pass nil for oLog when the host should be the single OTel emission point.
func NewPluginTransportLogger(hc hclog.Logger, oLog otellog.Logger) Logger {
	w := &zerologToHCLogWriter{
		logger: hc,
	}

	z := zerolog.New(w).
		With().
		Timestamp().
		Caller().
		Stack().
		Logger()

	return &zerologAdapter{
		logger: z,
		ot:     oLog,
	}
}

// zerologToHCLogWriter receives structured zerolog JSON events and forwards
// them through hclog. This is the plugin-side transport bridge.
type zerologToHCLogWriter struct {
	logger hclog.Logger
}

func (w *zerologToHCLogWriter) Write(p []byte) (int, error) {
	return w.WriteLevel(zerolog.NoLevel, p)
}

func (w *zerologToHCLogWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	if w == nil || w.logger == nil {
		return len(p), nil
	}

	raw := bytes.TrimSpace(p)
	if len(raw) == 0 {
		return len(p), nil
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		w.logger.Log(zerologLevelToHCLog(level), string(raw))
		return len(p), nil
	}

	msg, _ := fields[zerolog.MessageFieldName].(string)

	if lv, ok := fields[zerolog.LevelFieldName].(string); ok {
		level = parseZerologLevel(lv)
	}

	delete(fields, zerolog.MessageFieldName)
	delete(fields, zerolog.LevelFieldName)

	args := make([]any, 0, len(fields)*2+2)

	// Preserve the original zerolog event so the host can retain full fidelity.
	args = append(args, hclogRawJSONField, string(raw))

	for k, v := range fields {
		args = append(args, k, v)
	}

	w.logger.Log(zerologLevelToHCLog(level), msg, args...)
	return len(p), nil
}

func (w *zerologToHCLogWriter) Close() error {
	if c, ok := w.logger.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// hcLogToLogger receives hclog events and emits them through your Logger.
type hcLogToLogger struct {
	base        Logger
	name        string
	level       hclog.Level
	impliedArgs []any
}

var _ hclog.Logger = (*hcLogToLogger)(nil)

func (l *hcLogToLogger) Log(level hclog.Level, msg string, args ...any) {
	if l == nil || l.base == nil || !l.enabled(level) {
		return
	}

	allArgs := append([]any{}, l.impliedArgs...)
	allArgs = append(allArgs, args...)

	raw, remaining := extractRawJSON(allArgs)
	if raw != "" {
		l.logForwardedZerolog(raw, remaining)
		return
	}

	ev := l.event(level)

	if l.name != "" {
		ev.Str(hclogLoggerField, l.name)
	}

	addHCLogArgsToEvent(ev, allArgs)
	ev.Msg(msg)
}

func (l *hcLogToLogger) Trace(msg string, args ...any) { l.Log(hclog.Trace, msg, args...) }
func (l *hcLogToLogger) Debug(msg string, args ...any) { l.Log(hclog.Debug, msg, args...) }
func (l *hcLogToLogger) Info(msg string, args ...any)  { l.Log(hclog.Info, msg, args...) }
func (l *hcLogToLogger) Warn(msg string, args ...any)  { l.Log(hclog.Warn, msg, args...) }
func (l *hcLogToLogger) Error(msg string, args ...any) { l.Log(hclog.Error, msg, args...) }

func (l *hcLogToLogger) IsTrace() bool { return l.enabled(hclog.Trace) }
func (l *hcLogToLogger) IsDebug() bool { return l.enabled(hclog.Debug) }
func (l *hcLogToLogger) IsInfo() bool  { return l.enabled(hclog.Info) }
func (l *hcLogToLogger) IsWarn() bool  { return l.enabled(hclog.Warn) }
func (l *hcLogToLogger) IsError() bool { return l.enabled(hclog.Error) }

func (l *hcLogToLogger) ImpliedArgs() []any {
	out := make([]any, len(l.impliedArgs))
	copy(out, l.impliedArgs)
	return out
}

func (l *hcLogToLogger) With(args ...any) hclog.Logger {
	cp := *l
	cp.impliedArgs = append(append([]any{}, l.impliedArgs...), args...)
	return &cp
}

func (l *hcLogToLogger) Name() string {
	return l.name
}

func (l *hcLogToLogger) Named(name string) hclog.Logger {
	cp := *l
	if cp.name == "" {
		cp.name = name
	} else if name != "" {
		cp.name += "." + name
	}
	return &cp
}

func (l *hcLogToLogger) ResetNamed(name string) hclog.Logger {
	cp := *l
	cp.name = name
	return &cp
}

func (l *hcLogToLogger) SetLevel(level hclog.Level) {
	l.level = level
}

func (l *hcLogToLogger) GetLevel() hclog.Level {
	return l.level
}

func (l *hcLogToLogger) StandardLogger(opts *hclog.StandardLoggerOptions) *stdlog.Logger {
	return stdlog.New(l.StandardWriter(opts), "", 0)
}

func (l *hcLogToLogger) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	return &hcLogStdWriter{
		logger: l,
		opts:   opts,
	}
}

func (l *hcLogToLogger) event(level hclog.Level) Event {
	switch level {
	case hclog.Trace:
		return l.base.Trace()
	case hclog.Debug:
		return l.base.Debug()
	case hclog.Info:
		return l.base.Info()
	case hclog.Warn:
		return l.base.Warn()
	case hclog.Error:
		return l.base.Error()
	default:
		return l.base.Log()
	}
}

func (l *hcLogToLogger) enabled(level hclog.Level) bool {
	if l == nil {
		return false
	}
	return level >= l.level
}

type hcLogStdWriter struct {
	logger *hcLogToLogger
	opts   *hclog.StandardLoggerOptions
}

func (w *hcLogStdWriter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if line == "" {
		return len(p), nil
	}

	level := hclog.Info
	msg := line

	if w.opts != nil && w.opts.InferLevels {
		level, msg = inferHCLogLevel(line)
	}

	w.logger.Log(level, msg)
	return len(p), nil
}

func addHCLogArgsToEvent(ev Event, args []any) {
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok || key == "" {
			key = "extra"
		}

		var val any
		if i+1 < len(args) {
			val = args[i+1]
		}

		switch v := val.(type) {
		case string:
			ev.Str(key, v)
		case bool:
			ev.Bool(key, v)
		case int:
			ev.Int(key, v)
		case int8:
			ev.Int8(key, v)
		case int16:
			ev.Int16(key, v)
		case int32:
			ev.Int32(key, v)
		case int64:
			ev.Int64(key, v)
		case uint:
			ev.Uint(key, v)
		case uint8:
			ev.Uint8(key, v)
		case uint16:
			ev.Uint16(key, v)
		case uint32:
			ev.Uint32(key, v)
		case uint64:
			ev.Uint64(key, v)
		case float32:
			ev.Float32(key, v)
		case float64:
			ev.Float64(key, v)
		case time.Duration:
			ev.Dur(key, v)
		case time.Time:
			ev.Time(key, v)
		case error:
			ev.AnErr(key, v)
		case []byte:
			ev.Bytes(key, v)
		case json.RawMessage:
			ev.RawJSON(key, v)
		default:
			ev.Any(key, v)
		}
	}
}

func extractRawJSON(args []any) (string, []any) {
	out := make([]any, 0, len(args))

	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			out = append(out, args[i])
			continue
		}

		key, _ := args[i].(string)
		if key == hclogRawJSONField {
			switch v := args[i+1].(type) {
			case string:
				return v, append(out, args[i+2:]...)
			case []byte:
				return string(v), append(out, args[i+2:]...)
			case json.RawMessage:
				return string(v), append(out, args[i+2:]...)
			}
		}

		out = append(out, args[i], args[i+1])
	}

	return "", out
}

func zerologLevelToHCLog(level zerolog.Level) hclog.Level {
	switch level {
	case zerolog.TraceLevel:
		return hclog.Trace
	case zerolog.DebugLevel:
		return hclog.Debug
	case zerolog.InfoLevel:
		return hclog.Info
	case zerolog.WarnLevel:
		return hclog.Warn
	case zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel:
		return hclog.Error
	default:
		return hclog.Info
	}
}

func parseZerologLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

func inferHCLogLevel(line string) (hclog.Level, string) {
	s := strings.TrimSpace(line)
	upper := strings.ToUpper(s)

	prefixes := []struct {
		prefix string
		level  hclog.Level
	}{
		{"[TRACE]", hclog.Trace},
		{"TRACE", hclog.Trace},
		{"[DEBUG]", hclog.Debug},
		{"DEBUG", hclog.Debug},
		{"[INFO]", hclog.Info},
		{"INFO", hclog.Info},
		{"[WARN]", hclog.Warn},
		{"WARN", hclog.Warn},
		{"WARNING", hclog.Warn},
		{"[ERROR]", hclog.Error},
		{"ERROR", hclog.Error},
	}

	for _, p := range prefixes {
		if strings.HasPrefix(upper, p.prefix) {
			return p.level, strings.TrimSpace(s[len(p.prefix):])
		}
	}

	return hclog.Info, s
}

func (l *hcLogToLogger) logForwardedZerolog(raw string, transportArgs []any) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		ev := l.base.Warn()

		if l.name != "" {
			ev.Str(hclogLoggerField, l.name)
		}

		ev.Str(hclogRawJSONField, raw)
		addTransportArgsToEvent(ev, transportArgs)
		ev.Msg("failed to parse forwarded zerolog event")
		return
	}

	level := parseForwardedZerologLevel(fields)
	msg := takeString(fields, zerolog.MessageFieldName)

	delete(fields, zerolog.LevelFieldName)
	delete(fields, zerolog.MessageFieldName)

	ev := l.event(zerologLevelToHCLog(level))

	if l.name != "" {
		ev.Str(hclogLoggerField, l.name)
	}

	for key, val := range fields {
		addAnyToEvent(ev, key, val)
	}

	addTransportArgsToEvent(ev, transportArgs)

	ev.Msg(msg)
}

func parseForwardedZerologLevel(fields map[string]any) zerolog.Level {
	level, _ := fields[zerolog.LevelFieldName].(string)
	return parseZerologLevel(level)
}

func takeString(fields map[string]any, key string) string {
	val, _ := fields[key].(string)
	return val
}

func addTransportArgsToEvent(ev Event, args []any) {
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			continue
		}

		key, ok := args[i].(string)
		if !ok || key == "" {
			continue
		}

		switch key {
		case hclogRawJSONField,
			"@level",
			"@message",
			"@module",
			"@timestamp",
			"timestamp":
			continue
		}

		addAnyToEvent(ev, "plugin_transport."+key, args[i+1])
	}
}

func addAnyToEvent(ev Event, key string, val any) {
	switch v := val.(type) {
	case string:
		ev.Str(key, v)
	case bool:
		ev.Bool(key, v)
	case int:
		ev.Int(key, v)
	case int8:
		ev.Int8(key, v)
	case int16:
		ev.Int16(key, v)
	case int32:
		ev.Int32(key, v)
	case int64:
		ev.Int64(key, v)
	case uint:
		ev.Uint(key, v)
	case uint8:
		ev.Uint8(key, v)
	case uint16:
		ev.Uint16(key, v)
	case uint32:
		ev.Uint32(key, v)
	case uint64:
		ev.Uint64(key, v)
	case float32:
		ev.Float32(key, v)
	case float64:
		if v == float64(int64(v)) {
			ev.Int64(key, int64(v))
		} else {
			ev.Float64(key, v)
		}
	case time.Duration:
		ev.Dur(key, v)
	case time.Time:
		ev.Time(key, v)
	case error:
		ev.AnErr(key, v)
	case []byte:
		ev.Bytes(key, v)
	case json.RawMessage:
		ev.RawJSON(key, v)
	case []any, map[string]any:
		ev.Any(key, v)
	case nil:
		ev.Any(key, nil)
	default:
		ev.Any(key, v)
	}
}
