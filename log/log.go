package log

/*
logger allows for logging at the following levels (from highest to lowest):

panic (zerolog.PanicLevel, 5)
fatal (zerolog.FatalLevel, 4)
error (zerolog.ErrorLevel, 3)
warn (zerolog.WarnLevel, 2)
info (zerolog.InfoLevel, 1)
debug (zerolog.DebugLevel, 0)
trace (zerolog.TraceLevel, -1)
*/

// adapted from zerolog
import (
	"bytes"
	"context"
	"fmt"
	"io"
	stdlog "log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
	"github.com/samber/oops"
	oopszerolog "github.com/samber/oops/loggers/zerolog"
	otellog "go.opentelemetry.io/otel/log"
)

type Level int32

const (
	TraceLevel Level = iota - 1
	DebugLevel
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
	PanicLevel
)

func (l Level) String() string {
	switch l {
	case TraceLevel:
		return "trace"
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	case FatalLevel:
		return "fatal"
	case PanicLevel:
		return "panic"
	default:
		return "unknown"
	}
}

func (l *Level) UnmarshalText(text []byte) error {
	value := strings.ToLower(strings.TrimSpace(string(text)))

	switch value {
	case "trace":
		*l = TraceLevel
	case "debug":
		*l = DebugLevel
	case "info":
		*l = InfoLevel
	case "warn":
		*l = WarnLevel
	case "error":
		*l = ErrorLevel
	case "fatal":
		*l = FatalLevel
	case "panic":
		*l = PanicLevel

	default:
		return oops.Errorf("invalid mode %q: expected trace, debug, info, warn, error, fatal, or panic", value)
	}
	return nil
}

func (l Level) MarshalText() ([]byte, error) {
	switch l {
	case TraceLevel, DebugLevel, InfoLevel, WarnLevel, ErrorLevel, FatalLevel, PanicLevel:
		return []byte(l.String()), nil
	default:
		return nil, oops.Errorf("cannot marshal invalid mode %q", l)
	}
}

func ParseLogLevel(level string) Level {
	switch level {
	case "trace":
		return TraceLevel
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn":
		return WarnLevel
	case "error":
		return ErrorLevel
	case "fatal":
		return FatalLevel
	case "panic":
		return PanicLevel
	default:
		return InfoLevel
	}
}

type additionalFieldAdder[T any] interface {
	AnErr(key string, err error) T
	Any(key string, val any) T
	Bool(key string, val bool) T
	Bools(key string, vals []bool) T
	Bytes(key string, val []byte) T
	Ctx(ctx context.Context) T
	Dur(key string, val time.Duration) T
	Durs(key string, vals []time.Duration) T
	Err(err error) T
	Errs(key string, errs []error) T
	Fields(fields any) T
	Float32(key string, val float32) T
	Float64(key string, val float64) T
	Floats32(key string, vals []float32) T
	Floats64(key string, vals []float64) T
	Hex(key string, val []byte) T
	IPAddr(key string, val net.IP) T
	IPPrefix(key string, val net.IPNet) T
	Int(key string, val int) T
	Int16(key string, val int16) T
	Int32(key string, val int32) T
	Int64(key string, val int64) T
	Int8(key string, val int8) T
	Interface(key string, val any) T
	Ints(key string, vals []int) T
	Ints16(key string, vals []int16) T
	Ints32(key string, vals []int32) T
	Ints64(key string, vals []int64) T
	Ints8(key string, vals []int8) T
	MACAddr(key string, val net.HardwareAddr) T
	RawCBOR(key string, val []byte) T
	RawJSON(key string, val []byte) T
	Str(key string, val string) T
	Strs(key string, vals []string) T
	Time(key string, val time.Time) T
	TimeDiff(key string, t, start time.Time) T
	Times(key string, vals []time.Time) T
	Uint(key string, val uint) T
	Uint16(key string, val uint16) T
	Uint32(key string, val uint32) T
	Uint64(key string, val uint64) T
	Uint8(key string, val uint8) T
	Uints(key string, vals []uint) T
	Uints16(key string, vals []uint16) T
	Uints32(key string, vals []uint32) T
	Uints64(key string, vals []uint64) T
	Uints8(key string, vals []uint8) T
}

type Event interface {
	additionalFieldAdder[Event]
	Msg(msg string)
	Msgf(format string, v ...any)
	Send()
}

type Context interface {
	additionalFieldAdder[Context]
	Logger() Logger
}

// Logger defines the interface for structured logging
type Logger interface {
	Debug() Event
	Err(err error) Event
	Error() Event
	Fatal() Event
	GetLevel() Level
	Info() Event
	Level(lvl Level) Logger
	Log() Event
	Output(w io.Writer) Logger
	Panic() Event
	Trace() Event
	Warn() Event
	With() Context
	WithLevel(level Level) Event
}

var GlobalLog = GetDefaultLogger(os.Stdout, nil)

func init() {
	zerolog.ErrorStackMarshaler = oopszerolog.OopsStackMarshaller
	//zerolog.ErrorMarshalFunc = oopszerolog.OopsMarshalFunc
}

func GetDefaultLogger(w io.Writer, oLog otellog.Logger) Logger {

	// create a thread-safe, lock-free, non-blocking writer
	// this prevents logging from blocking the application
	// buffer size of 1000 messages, flushing every 10ms
	wr := diode.NewWriter(w, 1000, 10*time.Millisecond, func(missed int) {
		fmt.Printf("Logger Dropped %d messages\n", missed)
	})

	// configure zerolog to use the diode writer with console formatting
	consoleWriter := zerolog.ConsoleWriter{
		Out:           wr,
		FieldsExclude: []string{"stack"},
		FormatExtra: func(m map[string]any, buffer *bytes.Buffer) error {
			if stack, ok := m["stack"]; ok {
				if err, ok := m["error"]; ok && stack == err {
					return nil
				}
				buffer.WriteByte('\n')
				buffer.WriteString(stack.(string))
			}
			return nil
		},
	}

	logger := zerolog.New(consoleWriter).With().Timestamp().Caller().Stack().Logger()

	// configure the standard library logger to use our zerolog instance
	stdlog.SetFlags(0)
	stdlog.SetOutput(logger)
	return &zerologAdapter{logger: logger, ot: oLog}
}
