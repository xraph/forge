package logger

import (
	"github.com/xraph/go-utils/log"
)

type LogLevel = log.LogLevel

const (
	LevelInfo  = log.LevelInfo
	LevelWarn  = log.LevelWarn
	LevelError = log.LevelError
	LevelFatal = log.LevelFatal
	LevelDebug = log.LevelDebug
)

// Config is the full logger construction surface.
type Config = log.Config

// Format selects the output encoder.
type Format = log.Format

const (
	FormatAuto   = log.FormatAuto
	FormatPretty = log.FormatPretty
	FormatJSON   = log.FormatJSON
)

// New creates a logger from a full Config.
func New(cfg Config) Logger { return log.New(cfg) }

// NewLogger creates a logger from the configuration-file struct.
func NewLogger(config LoggingConfig) Logger { return log.NewLogger(config) }

// NewDevelopmentLogger creates a pretty logger at debug level.
//
// The Config spelled out here is exactly what go-utils' deprecated
// NewDevelopmentLogger builds, so the output is unchanged.
func NewDevelopmentLogger() Logger {
	return log.New(Config{Format: FormatPretty, Level: LevelDebug, AddCaller: true})
}

// NewProductionLogger creates a JSON logger.
//
// Mirrors go-utils' deprecated NewProductionLogger field for field.
func NewProductionLogger() Logger {
	return log.New(Config{Format: FormatJSON, Level: LevelInfo, AddCaller: true})
}

// NewNoopLogger creates a logger that discards everything.
func NewNoopLogger() Logger { return log.NewNoopLogger() }

func GetGlobalLogger() Logger { return log.GetGlobalLogger() }

func SetGlobalLogger(logger Logger) { log.SetGlobalLogger(logger) }

// ErrorHandler provides a callback-based error handler with logging.
type ErrorHandler = log.ErrorHandler

// LoggingWriter is an io.Writer that logs each write.
type LoggingWriter = log.LoggingWriter
