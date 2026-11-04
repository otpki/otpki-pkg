package log

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

type zerologAdapter struct {
	logger zerolog.Logger
	ot     otellog.Logger
}

type zerologEvent struct {
	event *zerolog.Event
	level zerolog.Level
	ctx   context.Context //nolint:containedctx // Each event retains its emission context for OpenTelemetry forwarding.
	attrs []attribute.KeyValue
	ot    otellog.Logger
}

type zerologContext struct {
	ctx zerolog.Context
	ot  otellog.Logger
}

// ensure implementations satisfy interfaces
var (
	_ Logger  = (*zerologAdapter)(nil)
	_ Event   = (*zerologEvent)(nil)
	_ Context = (*zerologContext)(nil)
)

func (z *zerologAdapter) Trace() Event { return newEvent(z.logger.Trace(), zerolog.TraceLevel, z.ot) }

func (z *zerologAdapter) Debug() Event { return newEvent(z.logger.Debug(), zerolog.DebugLevel, z.ot) }
func (z *zerologAdapter) Info() Event  { return newEvent(z.logger.Info(), zerolog.InfoLevel, z.ot) }
func (z *zerologAdapter) Warn() Event  { return newEvent(z.logger.Warn(), zerolog.WarnLevel, z.ot) }
func (z *zerologAdapter) Error() Event { return newEvent(z.logger.Error(), zerolog.ErrorLevel, z.ot) }

func (z *zerologAdapter) Fatal() Event { return newEvent(z.logger.Fatal(), zerolog.FatalLevel, z.ot) }

func (z *zerologAdapter) Panic() Event { return newEvent(z.logger.Panic(), zerolog.PanicLevel, z.ot) }
func (z *zerologAdapter) Log() Event   { return newEvent(z.logger.Log(), zerolog.InfoLevel, z.ot) }
func (z *zerologAdapter) WithLevel(level Level) Event {
	zLevel := logLevelToZerolog(level)
	return newEvent(z.logger.WithLevel(zLevel), zLevel, z.ot)
}

func (z *zerologAdapter) Err(err error) Event {
	if err != nil {
		return newEvent(z.logger.Error().Err(err), zerolog.DebugLevel, z.ot)
	}
	return newEvent(z.logger.Info(), zerolog.DebugLevel, z.ot)
}
func (z *zerologAdapter) With() Context { return &zerologContext{ctx: z.logger.With(), ot: z.ot} }
func (z *zerologAdapter) GetLevel() Level {
	return logLevelFromZerolog(z.logger.GetLevel())
}

func (z *zerologAdapter) Level(level Level) Logger {
	return &zerologAdapter{logger: z.logger.Level(logLevelToZerolog(level)), ot: z.ot}
}

func (z *zerologAdapter) Output(w io.Writer) Logger {
	return &zerologAdapter{logger: z.logger.Output(w), ot: z.ot}
}

func newEvent(event *zerolog.Event, level zerolog.Level, otelLogger otellog.Logger) *zerologEvent {
	return &zerologEvent{
		event: event,
		level: level,
		ctx:   event.GetCtx(),
		attrs: []attribute.KeyValue{},
		ot:    otelLogger,
	}
}

func (e *zerologEvent) Ctx(ctx context.Context) Event {
	if e == nil {
		return e
	}
	e.event.Ctx(ctx)
	e.ctx = e.event.GetCtx()
	return e
}

func (e *zerologEvent) Str(key, val string) Event {
	e.event = e.event.Str(key, val)
	e.attrs = append(e.attrs, kvStr(key, val))
	return e
}

func (e *zerologEvent) Int(key string, val int) Event {
	e.event = e.event.Int(key, val)
	e.attrs = append(e.attrs, kvStr(key, strconv.Itoa(val)))
	return e
}

func (e *zerologEvent) Int64(key string, val int64) Event {
	e.event = e.event.Int64(key, val)
	e.attrs = append(e.attrs, kvStr(key, fmt.Sprintf("%d", val)))
	return e
}

func (e *zerologEvent) Uint64(key string, val uint64) Event {
	e.event = e.event.Uint64(key, val)
	e.attrs = append(e.attrs, kvStr(key, fmt.Sprintf("%d", val)))
	return e
}

func (e *zerologEvent) Float64(key string, val float64) Event {
	e.event = e.event.Float64(key, val)
	e.attrs = append(e.attrs, kvStr(key, fmt.Sprintf("%f", val)))
	return e
}

func (e *zerologEvent) Int32(key string, val int32) Event {
	e.event = e.event.Int32(key, val)
	e.attrs = append(e.attrs, kvStr(key, fmt.Sprintf("%d", val)))
	return e
}

func (e *zerologEvent) Uint32(key string, val uint32) Event {
	e.event = e.event.Uint32(key, val)
	e.attrs = append(e.attrs, kvStr(key, fmt.Sprintf("%d", val)))
	return e
}

func (e *zerologEvent) Float32(key string, val float32) Event {
	e.event = e.event.Float32(key, val)
	e.attrs = append(e.attrs, kvStr(key, fmt.Sprintf("%f", val)))
	return e
}

func (e *zerologEvent) Bool(key string, val bool) Event {
	e.event = e.event.Bool(key, val)
	e.attrs = append(e.attrs, kvStr(key, fmt.Sprintf("%t", val)))
	return e
}

func (e *zerologEvent) Err(err error) Event {
	e.event = e.event.Err(err)
	e.attrs = append(e.attrs, kvStr("error", err.Error()))
	return e
}

func (e *zerologEvent) Time(key string, val time.Time) Event {
	e.event = e.event.Time(key, val)
	e.attrs = append(e.attrs, kvStr(key, val.String()))
	return e
}

func (e *zerologEvent) Dur(key string, val time.Duration) Event {
	e.event = e.event.Dur(key, val)
	e.attrs = append(e.attrs, kvStr(key, val.String()))
	return e
}

func (e *zerologEvent) Interface(key string, val any) Event {
	e.event = e.event.Interface(key, val)
	return e
}

func (e *zerologEvent) Stringer(key string, val fmt.Stringer) Event {
	e.event = e.event.Stringer(key, val)
	e.attrs = append(e.attrs, kvStr(key, val.String()))
	return e
}

func (e *zerologEvent) Stack() Event {
	e.event = e.event.Stack()
	return e
}

func (e *zerologEvent) Msg(msg string) {
	e.addTraceContextFields()
	e.event.CallerSkipFrame(1).Msg(msg)
	e.emit(msg)
}

func (e *zerologEvent) Msgf(format string, v ...any) {
	e.addTraceContextFields()
	e.event.CallerSkipFrame(1).Msgf(format, v...)
	e.emit(fmt.Sprintf(format, v...))
}

func (e *zerologEvent) Send() {
	e.addTraceContextFields()
	e.event.Send()
	e.emit("")
}

func (e *zerologEvent) addTraceContextFields() {
	if e == nil || e.event == nil {
		return
	}

	sc := trace.SpanContextFromContext(e.event.GetCtx())
	if !sc.IsValid() {
		return
	}

	traceID := sc.TraceID().String()
	spanID := sc.SpanID().String()

	e.event = e.event.
		Str("trace_id", traceID).
		Str("span_id", spanID)

	e.attrs = appendIfMissingOTelString(e.attrs, "trace_id", traceID)
	e.attrs = appendIfMissingOTelString(e.attrs, "span_id", spanID)
}

func appendIfMissingOTelString(
	attrs []attribute.KeyValue,
	key string,
	val string,
) []attribute.KeyValue {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attrs
		}
	}
	return append(attrs, kvStr(key, val))
}

func (e *zerologEvent) emit(msg string) {
	if e.ot == nil {
		return
	}

	r := otellog.Record{}
	now := time.Now()

	r.SetTimestamp(now)
	r.SetSeverity(convertLevelZtoOtel(e.level))

	if msg != "" {
		r.SetBody(attribute.StringValue(msg))
	}

	attrs := append([]attribute.KeyValue(nil), e.attrs...)

	if sc := trace.SpanContextFromContext(e.event.GetCtx()); sc.IsValid() {
		attrs = appendIfMissingOTelString(attrs, "trace_id", sc.TraceID().String())
		attrs = appendIfMissingOTelString(attrs, "span_id", sc.SpanID().String())
	}

	if len(attrs) > 0 {
		r.AddAttributes(attrs...)
	}

	e.ot.Emit(e.ctx, r)
}

func kvStr(key string, val string) attribute.KeyValue {
	return attribute.String(key, val)
}

func (e *zerologEvent) AnErr(key string, err error) Event {
	e.event = e.event.AnErr(key, err)
	return e
}

func (e *zerologEvent) Any(key string, val any) Event {
	e.event = e.event.Any(key, val)
	return e
}

func (e *zerologEvent) Bools(key string, vals []bool) Event {
	e.event = e.event.Bools(key, vals)
	return e
}

func (e *zerologEvent) Bytes(key string, val []byte) Event {
	e.event = e.event.Bytes(key, val)
	return e
}

func (e *zerologEvent) Durs(key string, vals []time.Duration) Event {
	e.event = e.event.Durs(key, vals)
	return e
}

func (e *zerologEvent) Errs(key string, errs []error) Event {
	e.event = e.event.Errs(key, errs)
	return e
}

func (e *zerologEvent) Fields(fields any) Event {
	e.event = e.event.Fields(fields)
	return e
}

func (e *zerologEvent) Floats32(key string, vals []float32) Event {
	e.event = e.event.Floats32(key, vals)
	return e
}

func (e *zerologEvent) Floats64(key string, vals []float64) Event {
	e.event = e.event.Floats64(key, vals)
	return e
}

func (e *zerologEvent) Hex(key string, val []byte) Event {
	e.event = e.event.Hex(key, val)
	return e
}

func (e *zerologEvent) IPAddr(key string, val net.IP) Event {
	e.event = e.event.IPAddr(key, val)
	return e
}

func (e *zerologEvent) IPPrefix(key string, val net.IPNet) Event {
	e.event = e.event.IPPrefix(key, val)
	return e
}

func (e *zerologEvent) Int16(key string, val int16) Event {
	e.event = e.event.Int16(key, val)
	return e
}

func (e *zerologEvent) Int8(key string, val int8) Event {
	e.event = e.event.Int8(key, val)
	return e
}

func (e *zerologEvent) Ints(key string, vals []int) Event {
	e.event = e.event.Ints(key, vals)
	return e
}

func (e *zerologEvent) Ints16(key string, vals []int16) Event {
	e.event = e.event.Ints16(key, vals)
	return e
}

func (e *zerologEvent) Ints32(key string, vals []int32) Event {
	e.event = e.event.Ints32(key, vals)
	return e
}

func (e *zerologEvent) Ints64(key string, vals []int64) Event {
	e.event = e.event.Ints64(key, vals)
	return e
}

func (e *zerologEvent) Ints8(key string, vals []int8) Event {
	e.event = e.event.Ints8(key, vals)
	return e
}

func (e *zerologEvent) MACAddr(key string, val net.HardwareAddr) Event {
	e.event = e.event.MACAddr(key, val)
	return e
}

func (e *zerologEvent) RawCBOR(key string, val []byte) Event {
	e.event = e.event.RawCBOR(key, val)
	return e
}

func (e *zerologEvent) RawJSON(key string, val []byte) Event {
	e.event = e.event.RawJSON(key, val)
	return e
}

func (e *zerologEvent) Strs(key string, vals []string) Event {
	e.event = e.event.Strs(key, vals)
	return e
}

func (e *zerologEvent) TimeDiff(key string, t, start time.Time) Event {
	e.event = e.event.TimeDiff(key, t, start)
	return e
}

func (e *zerologEvent) Times(key string, vals []time.Time) Event {
	e.event = e.event.Times(key, vals)
	return e
}

func (e *zerologEvent) Uint(key string, val uint) Event {
	e.event = e.event.Uint(key, val)
	return e
}

func (e *zerologEvent) Uint16(key string, val uint16) Event {
	e.event = e.event.Uint16(key, val)
	return e
}

func (e *zerologEvent) Uint8(key string, val uint8) Event {
	e.event = e.event.Uint8(key, val)
	return e
}

func (e *zerologEvent) Uints(key string, vals []uint) Event {
	e.event = e.event.Uints(key, vals)
	return e
}

func (e *zerologEvent) Uints16(key string, vals []uint16) Event {
	e.event = e.event.Uints16(key, vals)
	return e
}

func (e *zerologEvent) Uints32(key string, vals []uint32) Event {
	e.event = e.event.Uints32(key, vals)
	return e
}

func (e *zerologEvent) Uints64(key string, vals []uint64) Event {
	e.event = e.event.Uints64(key, vals)
	return e
}

func (e *zerologEvent) Uints8(key string, vals []uint8) Event {
	e.event = e.event.Uints8(key, vals)
	return e
}

func (c *zerologContext) Str(key, val string) Context {
	c.ctx = c.ctx.Str(key, val)
	return c
}

func (c *zerologContext) Int(key string, val int) Context {
	c.ctx = c.ctx.Int(key, val)
	return c
}

func (c *zerologContext) Int64(key string, val int64) Context {
	c.ctx = c.ctx.Int64(key, val)
	return c
}

func (c *zerologContext) Bool(key string, val bool) Context {
	c.ctx = c.ctx.Bool(key, val)
	return c
}

func (c *zerologContext) Err(err error) Context {
	c.ctx = c.ctx.Err(err)
	return c
}

func (c *zerologContext) Time(key string, val time.Time) Context {
	c.ctx = c.ctx.Time(key, val)
	return c
}

func (c *zerologContext) Interface(key string, val any) Context {
	c.ctx = c.ctx.Interface(key, val)
	return c
}

func (c *zerologContext) AnErr(key string, err error) Context {
	c.ctx = c.ctx.AnErr(key, err)
	return c
}

func (c *zerologContext) Any(key string, val any) Context {
	c.ctx = c.ctx.Any(key, val)
	return c
}

func (c *zerologContext) Bools(key string, vals []bool) Context {
	c.ctx = c.ctx.Bools(key, vals)
	return c
}

func (c *zerologContext) Bytes(key string, val []byte) Context {
	c.ctx = c.ctx.Bytes(key, val)
	return c
}

func (c *zerologContext) Ctx(ctx context.Context) Context {
	c.ctx = c.ctx.Ctx(ctx)
	return c
}

func (c *zerologContext) Dur(key string, val time.Duration) Context {
	c.ctx = c.ctx.Dur(key, val)
	return c
}

func (c *zerologContext) Durs(key string, vals []time.Duration) Context {
	c.ctx = c.ctx.Durs(key, vals)
	return c
}

func (c *zerologContext) Errs(key string, errs []error) Context {
	c.ctx = c.ctx.Errs(key, errs)
	return c
}

func (c *zerologContext) Fields(fields any) Context {
	c.ctx = c.ctx.Fields(fields)
	return c
}

func (c *zerologContext) Float32(key string, val float32) Context {
	c.ctx = c.ctx.Float32(key, val)
	return c
}

func (c *zerologContext) Float64(key string, val float64) Context {
	c.ctx = c.ctx.Float64(key, val)
	return c
}

func (c *zerologContext) Floats32(key string, vals []float32) Context {
	c.ctx = c.ctx.Floats32(key, vals)
	return c
}

func (c *zerologContext) Floats64(key string, vals []float64) Context {
	c.ctx = c.ctx.Floats64(key, vals)
	return c
}

func (c *zerologContext) Hex(key string, val []byte) Context {
	c.ctx = c.ctx.Hex(key, val)
	return c
}

func (c *zerologContext) IPAddr(key string, val net.IP) Context {
	c.ctx = c.ctx.IPAddr(key, val)
	return c
}

func (c *zerologContext) IPPrefix(key string, val net.IPNet) Context {
	c.ctx = c.ctx.IPPrefix(key, val)
	return c
}

func (c *zerologContext) Int16(key string, val int16) Context {
	c.ctx = c.ctx.Int16(key, val)
	return c
}

func (c *zerologContext) Int32(key string, val int32) Context {
	c.ctx = c.ctx.Int32(key, val)
	return c
}

func (c *zerologContext) Int8(key string, val int8) Context {
	c.ctx = c.ctx.Int8(key, val)
	return c
}

func (c *zerologContext) Ints(key string, vals []int) Context {
	c.ctx = c.ctx.Ints(key, vals)
	return c
}

func (c *zerologContext) Ints16(key string, vals []int16) Context {
	c.ctx = c.ctx.Ints16(key, vals)
	return c
}

func (c *zerologContext) Ints32(key string, vals []int32) Context {
	c.ctx = c.ctx.Ints32(key, vals)
	return c
}

func (c *zerologContext) Ints64(key string, vals []int64) Context {
	c.ctx = c.ctx.Ints64(key, vals)
	return c
}

func (c *zerologContext) Ints8(key string, vals []int8) Context {
	c.ctx = c.ctx.Ints8(key, vals)
	return c
}

func (c *zerologContext) MACAddr(key string, val net.HardwareAddr) Context {
	c.ctx = c.ctx.MACAddr(key, val)
	return c
}

func (c *zerologContext) RawCBOR(key string, val []byte) Context {
	// zerolog.Context doesn't support RawCBOR, use Bytes instead
	c.ctx = c.ctx.Bytes(key, val)
	return c
}

func (c *zerologContext) RawJSON(key string, val []byte) Context {
	c.ctx = c.ctx.RawJSON(key, val)
	return c
}

func (c *zerologContext) Strs(key string, vals []string) Context {
	c.ctx = c.ctx.Strs(key, vals)
	return c
}

func (c *zerologContext) TimeDiff(key string, t, start time.Time) Context {
	// zerolog.Context doesn't support TimeDiff, compute duration instead
	c.ctx = c.ctx.Dur(key, t.Sub(start))
	return c
}

func (c *zerologContext) Times(key string, vals []time.Time) Context {
	c.ctx = c.ctx.Times(key, vals)
	return c
}

func (c *zerologContext) Uint(key string, val uint) Context {
	c.ctx = c.ctx.Uint(key, val)
	return c
}

func (c *zerologContext) Uint16(key string, val uint16) Context {
	c.ctx = c.ctx.Uint16(key, val)
	return c
}

func (c *zerologContext) Uint32(key string, val uint32) Context {
	c.ctx = c.ctx.Uint32(key, val)
	return c
}

func (c *zerologContext) Uint64(key string, val uint64) Context {
	c.ctx = c.ctx.Uint64(key, val)
	return c
}

func (c *zerologContext) Uint8(key string, val uint8) Context {
	c.ctx = c.ctx.Uint8(key, val)
	return c
}

func (c *zerologContext) Uints(key string, vals []uint) Context {
	c.ctx = c.ctx.Uints(key, vals)
	return c
}

func (c *zerologContext) Uints16(key string, vals []uint16) Context {
	c.ctx = c.ctx.Uints16(key, vals)
	return c
}

func (c *zerologContext) Uints32(key string, vals []uint32) Context {
	c.ctx = c.ctx.Uints32(key, vals)
	return c
}

func (c *zerologContext) Uints64(key string, vals []uint64) Context {
	c.ctx = c.ctx.Uints64(key, vals)
	return c
}

func (c *zerologContext) Uints8(key string, vals []uint8) Context {
	c.ctx = c.ctx.Uints8(key, vals)
	return c
}

func (c *zerologContext) Logger() Logger {
	return &zerologAdapter{logger: c.ctx.Logger(), ot: c.ot}
}

// GetZerologLogger extracts the underlying zerolog.Logger
// returns nil if the logger is not a zerolog-based implementation
func GetZerologLogger(l Logger) *zerolog.Logger {
	if adapter, ok := l.(*zerologAdapter); ok {
		return &adapter.logger
	}
	return nil
}

func logLevelToZerolog(level Level) zerolog.Level {
	switch level {
	case TraceLevel:
		return zerolog.TraceLevel
	case DebugLevel:
		return zerolog.DebugLevel
	case InfoLevel:
		return zerolog.InfoLevel
	case WarnLevel:
		return zerolog.WarnLevel
	case ErrorLevel:
		return zerolog.ErrorLevel
	case FatalLevel:
		return zerolog.FatalLevel
	case PanicLevel:
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

func logLevelFromZerolog(level zerolog.Level) Level {
	switch level {
	case zerolog.TraceLevel:
		return TraceLevel
	case zerolog.DebugLevel:
		return DebugLevel
	case zerolog.InfoLevel:
		return InfoLevel
	case zerolog.WarnLevel:
		return WarnLevel
	case zerolog.ErrorLevel:
		return ErrorLevel
	case zerolog.FatalLevel:
		return FatalLevel
	case zerolog.PanicLevel:
		return PanicLevel
	default:
		return InfoLevel
	}
}

func convertLevelZtoOtel(level zerolog.Level) otellog.Severity {
	switch level {
	case zerolog.TraceLevel:
		return otellog.SeverityTrace
	case zerolog.DebugLevel:
		return otellog.SeverityDebug
	case zerolog.InfoLevel:
		return otellog.SeverityInfo
	case zerolog.WarnLevel:
		return otellog.SeverityWarn
	case zerolog.ErrorLevel:
		return otellog.SeverityError
	case zerolog.PanicLevel, zerolog.FatalLevel:
		return otellog.SeverityFatal
	default:
		return otellog.SeverityUndefined
	}
}
