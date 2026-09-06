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

var (
	LoggerFromContext    = log.LoggerFromContext
	WithRequestID        = log.WithRequestID
	RequestIDFromContext = log.RequestIDFromContext
	WithTraceID          = log.WithTraceID
	TraceIDFromContext   = log.TraceIDFromContext
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
