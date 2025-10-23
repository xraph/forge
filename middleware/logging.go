package middleware

import (
	"net/http"
	"time"

	forge "github.com/xraph/forge"
)

// LoggingConfig defines configuration for logging middleware
type LoggingConfig struct {
	// IncludeHeaders includes request headers in logs
	IncludeHeaders bool

	// ExcludePaths defines paths to exclude from logging
	ExcludePaths []string

	// SensitiveHeaders defines headers to redact in logs
	SensitiveHeaders []string
}

// DefaultLoggingConfig returns default logging configuration
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		IncludeHeaders:   false,
		ExcludePaths:     []string{"/health", "/metrics"},
		SensitiveHeaders: []string{"Authorization", "Cookie", "Set-Cookie"},
	}
}

// Logging middleware logs HTTP requests with timing information
func Logging(logger forge.Logger) forge.Middleware {
	return LoggingWithConfig(logger, DefaultLoggingConfig())
}

// LoggingWithConfig middleware logs HTTP requests with custom configuration
func LoggingWithConfig(logger forge.Logger, config LoggingConfig) forge.Middleware {
	// Pre-compile exclude paths for performance
	excludeMap := make(map[string]bool)
	for _, path := range config.ExcludePaths {
		excludeMap[path] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip excluded paths
			if excludeMap[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Start timing
			start := time.Now()

			// Wrap response writer to capture status
			wrapped := NewResponseWriter(w)

			// Log request start
			logger.Info("request started")

			// Process request
			next.ServeHTTP(wrapped, r)

			// Calculate duration
			_ = time.Since(start)

			// Log request completion
			logger.Info("request completed")
		})
	}
}
