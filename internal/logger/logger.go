package logger

import (
	"github.com/xraph/go-utils/log"
	"go.uber.org/zap/zapcore"
)

type LogLevel = log.LogLevel

const (
	LevelInfo  = log.LevelInfo
	LevelWarn  = log.LevelWarn
	LevelError = log.LevelError
	LevelFatal = log.LevelFatal
	LevelDebug = log.LevelDebug
)

// NewLogger creates a new logger with the given configuration.
func NewLogger(config LoggingConfig) Logger {
	return log.NewLogger(config)
}

// NewDevelopmentLogger creates a development logger with enhanced colors.
func NewDevelopmentLogger() Logger {
	return log.NewDevelopmentLogger()
}

// NewDevelopmentLoggerWithLevel creates a development logger with specified level.
func NewDevelopmentLoggerWithLevel(level zapcore.Level) Logger {
	return log.NewDevelopmentLoggerWithLevel(level)
}

// NewProductionLogger creates a production logger.
func NewProductionLogger() Logger {
	return log.NewProductionLogger()
}

// NewNoopLogger creates a logger that does nothing.
func NewNoopLogger() Logger {
	return log.NewNoopLogger()
}

func GetGlobalLogger() Logger {
	return log.GetGlobalLogger()
}

func SetGlobalLogger(logger Logger) {
	log.SetGlobalLogger(logger)
}

// ErrorHandler provides a callback-based error handler with logging.
type ErrorHandler = log.ErrorHandler

// LoggingWriter is an io.Writer that logs each write.
type LoggingWriter = log.LoggingWriter
