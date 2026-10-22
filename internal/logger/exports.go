package logger

import (
	"github.com/xraph/go-utils/log"
)

// NewBeautifulLogger creates a logger that picks its format automatically.
//
// Deprecated: use New(Config{Name: name}).
func NewBeautifulLogger(name string) Logger { return log.NewBeautifulLogger(name) }

// NewBeautifulLoggerCompact creates a logger without caller information.
//
// Deprecated: use New(Config{Name: name}).
func NewBeautifulLoggerCompact(name string) Logger { return log.NewBeautifulLoggerCompact(name) }

// NewBeautifulLoggerMinimal creates a logger without caller information.
//
// Deprecated: use New(Config{Name: name}).
func NewBeautifulLoggerMinimal(name string) Logger { return log.NewBeautifulLoggerMinimal(name) }

// NewBeautifulLoggerJSON creates a JSON logger.
//
// Deprecated: use New(Config{Name: name, Format: FormatJSON}).
func NewBeautifulLoggerJSON(name string) Logger { return log.NewBeautifulLoggerJSON(name) }

// StructuredLog provides a fluent interface for structured logging.
type StructuredLog = log.StructuredLog

// TestLogger provides a test logger implementation.
type TestLogger = log.TestLogger

// LogEntry represents a log entry.
type LogEntry = log.LogEntry

// PerformanceMonitor helps monitor performance metrics.
type PerformanceMonitor = log.PerformanceMonitor

// Logger represents the logging interface.
type Logger = log.Logger

// SugarLogger provides a more flexible API.
type SugarLogger = log.SugarLogger

// Field represents a structured log field.
type Field = log.Field

// LoggingConfig represents logging configuration.
type LoggingConfig = log.LoggingConfig

// Record is one written log line, handed to every attached Sink. Its Fields
// are valid only during Emit.
type Record = log.Record

// Sink receives every line a logger writes, after the level check.
type Sink = log.Sink

// Flusher is implemented by a Sink that buffers. Fatal flushes it before the
// process exits.
type Flusher = log.Flusher

// ErrSinksUnsupported is returned by AttachSink for a logger that cannot carry
// sinks, such as the noop logger.
var ErrSinksUnsupported = log.ErrSinksUnsupported

// AttachSink adds s to the whole logger tree that l belongs to and returns the
// function that removes it again.
var AttachSink = log.AttachSink

var (
	LoggerFromContext    = log.LoggerFromContext
	WithRequestID        = log.WithRequestID
	RequestIDFromContext = log.RequestIDFromContext
	WithTraceID          = log.WithTraceID
	TraceIDFromContext   = log.TraceIDFromContext
	WithSpanID           = log.WithSpanID
	SpanIDFromContext    = log.SpanIDFromContext
	WithUserID           = log.WithUserID
	UserIDFromContext    = log.UserIDFromContext
)

// Re-export utility functions.
var (
	Track              = log.Track
	TrackWithLogger    = log.TrackWithLogger
	TrackWithFields    = log.TrackWithFields
	LogPanic           = log.LogPanic
	LogPanicWithFields = log.LogPanicWithFields
)

func NewTestLogger() log.Logger {
	return log.NewTestLogger()
}
