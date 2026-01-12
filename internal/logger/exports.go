package logger

import (
	"github.com/xraph/go-utils/log"
)

// FormatConfig controls what components are shown in the output.
type FormatConfig = log.FormatConfig

// DefaultFormatConfig provides sensible defaults.
func DefaultFormatConfig() FormatConfig {
	return log.DefaultFormatConfig()
}

// BeautifulLogger is a visually appealing alternative logger implementation
// with CLI-style output, caller information, and configurable formatting.
type BeautifulLogger = log.BeautifulLogger

// BeautifulColorScheme defines the color palette for beautiful output.
type BeautifulColorScheme = log.BeautifulColorScheme

// DefaultBeautifulColorScheme provides a modern minimalist color scheme.
func DefaultBeautifulColorScheme() BeautifulColorScheme {
	return *log.DefaultBeautifulColorScheme()
}

// NewBeautifulLogger creates a new beautiful logger with defaults.
func NewBeautifulLogger(name string) *BeautifulLogger {
	return log.NewBeautifulLogger(name)
}

// ============================================================================
// Constructor Shortcuts
// ============================================================================

// NewBeautifulLoggerCompact creates a compact logger optimized for high-frequency logs.
func NewBeautifulLoggerCompact(name string) *BeautifulLogger {
	return log.NewBeautifulLoggerCompact(name)
}

// NewBeautifulLoggerMinimal creates an ultra-minimal logger.
func NewBeautifulLoggerMinimal(name string) *BeautifulLogger {
	return log.NewBeautifulLoggerMinimal(name)
}

// NewBeautifulLoggerJSON creates a logger similar to JSON output (caller, fields, timestamp).
func NewBeautifulLoggerJSON(name string) *BeautifulLogger {
	return log.NewBeautifulLoggerJSON(name)
}

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
