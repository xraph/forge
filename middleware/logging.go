package middleware

import (
	"net/http"
	"strings"
	"time"

	forge "github.com/xraph/forge"
)

// redactedValue replaces the value of a sensitive header in logs.
const redactedValue = "[REDACTED]"

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

	// Pre-lower the sensitive header names once; HTTP header names are
	// case-insensitive, so the comparison must be too.
	sensitive := make(map[string]bool, len(config.SensitiveHeaders))
	for _, h := range config.SensitiveHeaders {
		sensitive[strings.ToLower(h)] = true
	}

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			// Skip excluded paths
			if excludeMap[ctx.Request().URL.Path] {
				return next(ctx)
			}

			r := ctx.Request()

			// Start timing
			start := time.Now()

			fields := []forge.Field{
				forge.F("method", r.Method),
				forge.F("path", r.URL.Path),
			}

			if config.IncludeHeaders {
				fields = append(fields, forge.F("headers", redactHeaders(r.Header, sensitive)))
			}

			// Log request start
			logger.Info("request started", fields...)

			// Process request
			err := next(ctx)

			// Log request completion, including the measured duration
			completion := []forge.Field{
				forge.F("method", r.Method),
				forge.F("path", r.URL.Path),
				forge.F("duration", time.Since(start)),
			}
			if err != nil {
				completion = append(completion, forge.F("error", err.Error()))
			}

			logger.Info("request completed", completion...)

			return err
		}
	}
}

// redactHeaders copies headers for logging, replacing the values of any header
// named in sensitive.
//
// Redaction happens here because the configured header list is the only place
// that knows which values are secrets — logging the raw header map would put
// bearer tokens and session cookies straight into the log.
func redactHeaders(header http.Header, sensitive map[string]bool) map[string]string {
	out := make(map[string]string, len(header))

	for name, values := range header {
		if sensitive[strings.ToLower(name)] {
			out[name] = redactedValue

			continue
		}

		out[name] = strings.Join(values, ", ")
	}

	return out
}
