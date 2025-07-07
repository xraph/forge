package logger

import (
	"context"

	"go.uber.org/zap"
)

// Logger represents the logging interface
type Logger interface {
	// Logging levels
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)

	// Formatted logging
	Debugf(template string, args ...interface{})
	Infof(template string, args ...interface{})
	Warnf(template string, args ...interface{})
	Errorf(template string, args ...interface{})
	Fatalf(template string, args ...interface{})

	// Context and enrichment
	With(fields ...Field) Logger
	WithContext(ctx context.Context) Logger
	Named(name string) Logger

	// Sugar logger
	Sugar() SugarLogger

	// Utilities
	Sync() error
}

// SugarLogger provides a more flexible API
type SugarLogger interface {
	Debugw(msg string, keysAndValues ...interface{})
	Infow(msg string, keysAndValues ...interface{})
	Warnw(msg string, keysAndValues ...interface{})
	Errorw(msg string, keysAndValues ...interface{})
	Fatalw(msg string, keysAndValues ...interface{})

	With(args ...interface{}) SugarLogger
}

// Field represents a structured log field
type Field interface {
	Key() string
	Value() interface{}
	// ZapField returns the underlying zap.Field for efficient conversion
	ZapField() zap.Field
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level       string `mapstructure:"level" yaml:"level" env:"FORGE_LOG_LEVEL"`
	Format      string `mapstructure:"format" yaml:"format" env:"FORGE_LOG_FORMAT"`
	Environment string `mapstructure:"environment" yaml:"environment" env:"FORGE_ENVIRONMENT"`
	Output      string `mapstructure:"output" yaml:"output" env:"FORGE_LOG_OUTPUT"`
}
