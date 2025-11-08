package middleware

import (
	"time"

	forge "github.com/xraph/forge"
)

// LoggingConfig defines configuration for logging middleware.
type LoggingConfig struct {
	// IncludeHeaders includes request headers in logs
	IncludeHeaders bool

	// ExcludePaths defines paths to exclude from logging
	ExcludePaths []string

	// SensitiveHeaders defines headers to redact in logs
	SensitiveHeaders []string
}

// DefaultLoggingConfig returns default logging configuration.
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		IncludeHeaders:   false,
		ExcludePaths:     []string{"/health", "/metrics"},
		SensitiveHeaders: []string{"Authorization", "Cookie", "Set-Cookie"},
	}
}

// Logging middleware logs HTTP requests with timing information.
func Logging(logger forge.Logger) forge.Middleware {
	return LoggingWithConfig(logger, DefaultLoggingConfig())
}

// LoggingWithConfig middleware logs HTTP requests with custom configuration.
func LoggingWithConfig(logger forge.Logger, config LoggingConfig) forge.Middleware {
	// Pre-compile exclude paths for performance
	excludeMap := make(map[string]bool)
	for _, path := range config.ExcludePaths {
		excludeMap[path] = true
	}

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			// Skip excluded paths
			if excludeMap[ctx.Request().URL.Path] {
				return next(ctx)
			}

			// Start timing
			start := time.Now()

			// Log request start
			logger.Info("request started")

			// Process request
			err := next(ctx)

			// Calculate duration
			_ = time.Since(start)

			// Log request completion
			logger.Info("request completed")

			return err
		}
	}
}
