package middleware

import (
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/xraph/forge/v0/pkg/cli"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// LoggingMiddleware provides logging for CLI commands
type LoggingMiddleware struct {
	*cli.BaseMiddleware
	logger common.Logger
	config LoggingConfig
}

// LoggingConfig contains logging middleware configuration
type LoggingConfig struct {
	LogCommands      bool     `yaml:"log_commands" json:"log_commands"`
	LogArgs          bool     `yaml:"log_args" json:"log_args"`
	LogDuration      bool     `yaml:"log_duration" json:"log_duration"`
	LogLevel         string   `yaml:"log_level" json:"log_level"`
	ExcludeFlags     []string `yaml:"exclude_flags" json:"exclude_flags"`
	ExcludeCommands  []string `yaml:"exclude_commands" json:"exclude_commands"`
	LogContext       bool     `yaml:"log_context" json:"log_context"`
	LogErrors        bool     `yaml:"log_errors" json:"log_errors"`
	LogSuccess       bool     `yaml:"log_success" json:"log_success"`
	StructuredOutput bool     `yaml:"structured_output" json:"structured_output"`
	TimestampFormat  string   `yaml:"timestamp_format" json:"timestamp_format"`
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(logger common.Logger) cli.CLIMiddleware {
	return NewLoggingMiddlewareWithConfig(logger, DefaultLoggingConfig())
}

// NewLoggingMiddlewareWithConfig creates logging middleware with custom config
func NewLoggingMiddlewareWithConfig(logger common.Logger, config LoggingConfig) cli.CLIMiddleware {
	return &LoggingMiddleware{
		BaseMiddleware: cli.NewBaseMiddleware("logging", 10),
		logger:         logger,
		config:         config,
	}
}

// Execute executes the logging middleware
func (lm *LoggingMiddleware) Execute(ctx cli.CLIContext, next func() error) error {
	// Skip if command is excluded
	if lm.shouldExcludeCommand(ctx) {
		return next()
	}

	start := time.Now()

	// Log command start
	if lm.config.LogCommands {
		lm.logCommandStart(ctx)
	}

	// Execute command
	err := next()
	duration := time.Since(start)

	// Log command completion
	if lm.config.LogDuration || lm.config.LogErrors || lm.config.LogSuccess {
		lm.logCommandComplete(ctx, err, duration)
	}

	return err
}

// logCommandStart logs the start of command execution
func (lm *LoggingMiddleware) logCommandStart(ctx cli.CLIContext) {
	fields := []common.LogField{
		logger.String("command", ctx.Command().Name()),
	}

	// Add arguments if enabled
	if lm.config.LogArgs {
		args := ctx.Args()
		if len(args) > 0 {
			fields = append(fields, logger.Strings("args", args))
		}

		// Add flags (filtered)
		flags := lm.getFilteredFlags(ctx)
		if len(flags) > 0 {
			fields = append(fields, logger.Any("flags", flags))
		}
	}

	// Add context if enabled
	if lm.config.LogContext {
		if userID := ctx.Get("user_id"); userID != nil {
			fields = append(fields, logger.String("user_id", userID.(string)))
		}
		if requestID := ctx.Get("request_id"); requestID != nil {
			fields = append(fields, logger.String("request_id", requestID.(string)))
		}
	}

	lm.logger.Info("command started", fields...)
}

// logCommandComplete logs the completion of command execution
func (lm *LoggingMiddleware) logCommandComplete(ctx cli.CLIContext, err error, duration time.Duration) {
	fields := []common.LogField{
		logger.String("command", ctx.Command().Name()),
	}

	if lm.config.LogDuration {
		fields = append(fields, logger.Duration("duration", duration))
	}

	if err != nil && lm.config.LogErrors {
		fields = append(fields, logger.Error(err))
		lm.logger.Error("command failed", fields...)
	} else if err == nil && lm.config.LogSuccess {
		lm.logger.Info("command completed", fields...)
	}
}

// shouldExcludeCommand checks if command should be excluded from logging
func (lm *LoggingMiddleware) shouldExcludeCommand(ctx cli.CLIContext) bool {
	commandName := ctx.Command().Name()
	for _, excluded := range lm.config.ExcludeCommands {
		if excluded == commandName {
			return true
		}
	}
	return false
}

// getFilteredFlags returns flags with sensitive values filtered out
func (lm *LoggingMiddleware) getFilteredFlags(ctx cli.CLIContext) map[string]interface{} {
	flags := make(map[string]interface{})

	ctx.Command().Flags().VisitAll(func(flag *pflag.Flag) {
		// Skip excluded flags
		for _, excluded := range lm.config.ExcludeFlags {
			if excluded == flag.Name {
				return
			}
		}

		// Skip sensitive flags by common patterns
		if lm.isSensitiveFlag(flag.Name) {
			flags[flag.Name] = "[REDACTED]"
		} else {
			flags[flag.Name] = flag.Value.String()
		}
	})

	return flags
}

// isSensitiveFlag checks if a flag contains sensitive information
func (lm *LoggingMiddleware) isSensitiveFlag(flagName string) bool {
	sensitivePatterns := []string{
		"password", "passwd", "pwd",
		"token", "key", "secret",
		"auth", "credential",
	}

	flagLower := strings.ToLower(flagName)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(flagLower, pattern) {
			return true
		}
	}

	return false
}

// DefaultLoggingConfig returns default logging configuration
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		LogCommands:      true,
		LogArgs:          true,
		LogDuration:      true,
		LogLevel:         "info",
		ExcludeFlags:     []string{"password", "token", "secret", "key"},
		ExcludeCommands:  []string{"completion"},
		LogContext:       true,
		LogErrors:        true,
		LogSuccess:       true,
		StructuredOutput: true,
		TimestampFormat:  time.RFC3339,
	}
}

// VerboseLoggingConfig returns configuration for verbose logging
func VerboseLoggingConfig() LoggingConfig {
	config := DefaultLoggingConfig()
	config.LogLevel = "debug"
	config.LogContext = true
	return config
}

// MinimalLoggingConfig returns configuration for minimal logging
func MinimalLoggingConfig() LoggingConfig {
	return LoggingConfig{
		LogCommands:     false,
		LogArgs:         false,
		LogDuration:     false,
		LogLevel:        "error",
		ExcludeFlags:    []string{},
		ExcludeCommands: []string{},
		LogContext:      false,
		LogErrors:       true,
		LogSuccess:      false,
	}
}
