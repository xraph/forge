package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/cron"
	"github.com/xraph/forge/v0/pkg/logger"
)

// LoggingMiddleware provides logging for job execution
type LoggingMiddleware struct {
	logger        common.Logger
	logLevel      LogLevel
	logExecution  bool
	logSuccess    bool
	logFailure    bool
	logRetry      bool
	logTimeout    bool
	logInput      bool
	logOutput     bool
	logDuration   bool
	logStackTrace bool
	maxOutputSize int
	sensitiveKeys []string
	tagExtractor  func(*cron.Job) map[string]interface{}
}

// LogLevel represents the logging level
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// LoggingConfig contains configuration for logging middleware
type LoggingConfig struct {
	LogLevel      LogLevel                               `json:"log_level" yaml:"log_level"`
	LogExecution  bool                                   `json:"log_execution" yaml:"log_execution"`
	LogSuccess    bool                                   `json:"log_success" yaml:"log_success"`
	LogFailure    bool                                   `json:"log_failure" yaml:"log_failure"`
	LogRetry      bool                                   `json:"log_retry" yaml:"log_retry"`
	LogTimeout    bool                                   `json:"log_timeout" yaml:"log_timeout"`
	LogInput      bool                                   `json:"log_input" yaml:"log_input"`
	LogOutput     bool                                   `json:"log_output" yaml:"log_output"`
	LogDuration   bool                                   `json:"log_duration" yaml:"log_duration"`
	LogStackTrace bool                                   `json:"log_stack_trace" yaml:"log_stack_trace"`
	MaxOutputSize int                                    `json:"max_output_size" yaml:"max_output_size"`
	SensitiveKeys []string                               `json:"sensitive_keys" yaml:"sensitive_keys"`
	TagExtractor  func(*cron.Job) map[string]interface{} `json:"-" yaml:"-"`
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(logger common.Logger, config *LoggingConfig) *LoggingMiddleware {
	if config == nil {
		config = &LoggingConfig{
			LogLevel:      LogLevelInfo,
			LogExecution:  true,
			LogSuccess:    true,
			LogFailure:    true,
			LogRetry:      true,
			LogTimeout:    true,
			LogInput:      false,
			LogOutput:     false,
			LogDuration:   true,
			LogStackTrace: false,
			MaxOutputSize: 1024,
			SensitiveKeys: []string{"password", "secret", "token", "key"},
		}
	}

	return &LoggingMiddleware{
		logger:        logger,
		logLevel:      config.LogLevel,
		logExecution:  config.LogExecution,
		logSuccess:    config.LogSuccess,
		logFailure:    config.LogFailure,
		logRetry:      config.LogRetry,
		logTimeout:    config.LogTimeout,
		logInput:      config.LogInput,
		logOutput:     config.LogOutput,
		logDuration:   config.LogDuration,
		logStackTrace: config.LogStackTrace,
		maxOutputSize: config.MaxOutputSize,
		sensitiveKeys: config.SensitiveKeys,
		tagExtractor:  config.TagExtractor,
	}
}

// Handle handles job execution with logging
func (lm *LoggingMiddleware) Handle(ctx context.Context, job *cron.Job, next func(ctx context.Context, job *cron.Job) error) error {
	start := time.Now()

	// Generate execution ID for correlation
	executionID := fmt.Sprintf("%s-%d", job.Definition.ID, start.UnixNano())

	// Create enriched context with execution ID
	enrichedCtx := context.WithValue(ctx, "execution_id", executionID)

	// Create base log fields
	baseFields := lm.createBaseLogFields(job, executionID)

	// Log execution start
	if lm.logExecution {
		lm.logWithLevel(LogLevelInfo, "job execution started", baseFields...)
	}

	// Log job input if enabled
	if lm.logInput {
		inputFields := lm.createInputLogFields(job)
		lm.logWithLevel(LogLevelDebug, "job input", append(baseFields, inputFields...)...)
	}

	// Execute the job
	err := next(enrichedCtx, job)

	// Calculate duration
	duration := time.Since(start)

	// Create result fields
	resultFields := lm.createResultLogFields(job, duration, err)
	allFields := append(baseFields, resultFields...)

	// Log based on result
	if err != nil {
		lm.logJobError(job, err, allFields, duration)
	} else {
		lm.logJobSuccess(job, allFields, duration)
	}

	return err
}

// createBaseLogFields creates base log fields for the job
func (lm *LoggingMiddleware) createBaseLogFields(job *cron.Job, executionID string) []logger.Field {
	fields := []logger.Field{
		logger.String("job_id", job.Definition.ID),
		logger.String("job_name", job.Definition.Name),
		logger.String("execution_id", executionID),
		logger.String("schedule", job.Definition.Schedule),
		logger.String("handler", fmt.Sprintf("%T", job.Definition.Handler)),
		logger.Int64("run_count", job.RunCount),
		logger.Int64("success_count", job.SuccessCount),
		logger.Int64("failure_count", job.FailureCount),
		logger.String("status", string(job.Status)),
		logger.Bool("enabled", job.Definition.Enabled),
		logger.Bool("singleton", job.Definition.Singleton),
		logger.Bool("distributed", job.Definition.Distributed),
		logger.Int("priority", job.Definition.Priority),
		logger.Int("max_retries", job.Definition.MaxRetries),
		logger.Duration("timeout", job.Definition.Timeout),
	}

	// Add node information if available
	if job.AssignedNode != "" {
		fields = append(fields, logger.String("assigned_node", job.AssignedNode))
	}
	if job.LeaderNode != "" {
		fields = append(fields, logger.String("leader_node", job.LeaderNode))
	}

	// Add tags
	if len(job.Definition.Tags) > 0 {
		for key, value := range job.Definition.Tags {
			fields = append(fields, logger.String(fmt.Sprintf("tag_%s", key), value))
		}
	}

	// Add dependencies
	if len(job.Definition.Dependencies) > 0 {
		fields = append(fields, logger.String("dependencies", strings.Join(job.Definition.Dependencies, ",")))
	}

	// Add custom tags from extractor
	if lm.tagExtractor != nil {
		customTags := lm.tagExtractor(job)
		for key, value := range customTags {
			fields = append(fields, logger.Any(fmt.Sprintf("custom_%s", key), value))
		}
	}

	return fields
}

// createInputLogFields creates log fields for job input
func (lm *LoggingMiddleware) createInputLogFields(job *cron.Job) []logger.Field {
	fields := []logger.Field{}

	// Log job configuration (sanitized)
	if job.Definition.Config != nil {
		sanitizedConfig := lm.sanitizeData(job.Definition.Config)
		fields = append(fields, logger.Any("job_config", sanitizedConfig))
	}

	// Log current execution context
	if job.CurrentExecution != nil {
		fields = append(fields,
			logger.String("current_execution_id", job.CurrentExecution.ID),
			logger.String("current_execution_status", string(job.CurrentExecution.Status)),
			logger.Int("current_execution_attempt", job.CurrentExecution.Attempt),
		)
	}

	// Log timing information
	if !job.NextRun.IsZero() {
		fields = append(fields, logger.Time("next_run", job.NextRun))
	}
	if !job.LastRun.IsZero() {
		fields = append(fields,
			logger.Time("last_run", job.LastRun),
			logger.Duration("time_since_last_run", time.Since(job.LastRun)),
		)
	}

	return fields
}

// createResultLogFields creates log fields for job execution result
func (lm *LoggingMiddleware) createResultLogFields(job *cron.Job, duration time.Duration, err error) []logger.Field {
	fields := []logger.Field{}

	// Always log duration if enabled
	if lm.logDuration {
		fields = append(fields, logger.Duration("duration", duration))
	}

	// Log success rate
	if job.RunCount > 0 {
		successRate := float64(job.SuccessCount) / float64(job.RunCount)
		failureRate := float64(job.FailureCount) / float64(job.RunCount)
		fields = append(fields,
			logger.Float64("success_rate", successRate),
			logger.Float64("failure_rate", failureRate),
		)
	}

	// Log performance metrics
	fields = append(fields,
		logger.Float64("duration_seconds", duration.Seconds()),
		logger.Int64("duration_milliseconds", duration.Milliseconds()),
		logger.Int64("duration_microseconds", duration.Microseconds()),
	)

	// Log error information if present
	if err != nil {
		fields = append(fields, logger.Error(err))

		// Log error type
		fields = append(fields, logger.String("error_type", fmt.Sprintf("%T", err)))

		// Log stack trace if enabled
		if lm.logStackTrace {
			if forgeErr, ok := err.(*common.ForgeError); ok && forgeErr.Stack != "" {
				fields = append(fields, logger.String("stack_trace", forgeErr.Stack))
			}
		}
	}

	return fields
}

// logJobSuccess logs successful job execution
func (lm *LoggingMiddleware) logJobSuccess(job *cron.Job, fields []logger.Field, duration time.Duration) {
	if !lm.logSuccess {
		return
	}

	message := fmt.Sprintf("job execution completed successfully in %v", duration)

	// Add performance context
	performanceLevel := lm.getPerformanceLevel(duration)
	fields = append(fields, logger.String("performance_level", performanceLevel))

	lm.logWithLevel(LogLevelInfo, message, fields...)
}

// logJobError logs failed job execution
func (lm *LoggingMiddleware) logJobError(job *cron.Job, err error, fields []logger.Field, duration time.Duration) {
	if !lm.logFailure {
		return
	}

	message := fmt.Sprintf("job execution failed after %v: %v", duration, err)

	// Determine log level based on error type
	logLevel := LogLevelError

	// Check for specific error types
	if forgeErr, ok := err.(*common.ForgeError); ok {
		switch forgeErr.Code {
		case common.ErrCodeTimeoutError:
			if lm.logTimeout {
				logLevel = LogLevelWarn
				message = fmt.Sprintf("job execution timed out after %v", duration)
			} else {
				return
			}
		case common.ErrCodeContextCancelled:
			logLevel = LogLevelWarn
			message = fmt.Sprintf("job execution cancelled after %v", duration)
		default:
			logLevel = LogLevelError
		}
	}

	// Add error context
	fields = append(fields,
		logger.String("error_category", lm.categorizeError(err)),
		logger.Bool("is_retryable", lm.isRetryableError(err)),
	)

	lm.logWithLevel(logLevel, message, fields...)
}

// OnSuccess logs successful job execution
func (lm *LoggingMiddleware) OnSuccess(ctx context.Context, job *cron.Job, execution *cron.JobExecution) {
	if !lm.logSuccess {
		return
	}

	fields := []logger.Field{
		logger.String("job_id", job.Definition.ID),
		logger.String("job_name", job.Definition.Name),
		logger.String("execution_id", execution.ID),
		logger.String("node_id", execution.NodeID),
		logger.Duration("duration", execution.Duration),
		logger.Int("attempt", execution.Attempt),
		logger.Time("start_time", execution.StartTime),
		logger.Time("end_time", execution.EndTime),
	}

	// Log output if enabled and available
	if lm.logOutput && execution.Output != "" {
		output := lm.truncateOutput(execution.Output)
		fields = append(fields, logger.String("output", output))
	}

	// Log execution metadata
	if len(execution.Metadata) > 0 {
		sanitizedMetadata := lm.sanitizeData(execution.Metadata)
		fields = append(fields, logger.Any("metadata", sanitizedMetadata))
	}

	message := fmt.Sprintf("job execution succeeded in %v", execution.Duration)
	lm.logWithLevel(LogLevelInfo, message, fields...)
}

// OnFailure logs failed job execution
func (lm *LoggingMiddleware) OnFailure(ctx context.Context, job *cron.Job, execution *cron.JobExecution, err error) {
	if !lm.logFailure {
		return
	}

	fields := []logger.Field{
		logger.String("job_id", job.Definition.ID),
		logger.String("job_name", job.Definition.Name),
		logger.String("execution_id", execution.ID),
		logger.String("node_id", execution.NodeID),
		logger.Duration("duration", execution.Duration),
		logger.Int("attempt", execution.Attempt),
		logger.Time("start_time", execution.StartTime),
		logger.Time("end_time", execution.EndTime),
		logger.Error(err),
		logger.String("error_type", fmt.Sprintf("%T", err)),
	}

	// Log error output
	if execution.Error != "" {
		fields = append(fields, logger.String("error_output", execution.Error))
	}

	// Log execution metadata
	if len(execution.Metadata) > 0 {
		sanitizedMetadata := lm.sanitizeData(execution.Metadata)
		fields = append(fields, logger.Any("metadata", sanitizedMetadata))
	}

	message := fmt.Sprintf("job execution failed after %v: %v", execution.Duration, err)
	lm.logWithLevel(LogLevelError, message, fields...)
}

// OnTimeout logs job timeout
func (lm *LoggingMiddleware) OnTimeout(ctx context.Context, job *cron.Job, execution *cron.JobExecution) {
	if !lm.logTimeout {
		return
	}

	fields := []logger.Field{
		logger.String("job_id", job.Definition.ID),
		logger.String("job_name", job.Definition.Name),
		logger.String("execution_id", execution.ID),
		logger.String("node_id", execution.NodeID),
		logger.Duration("duration", execution.Duration),
		logger.Duration("timeout", job.Definition.Timeout),
		logger.Int("attempt", execution.Attempt),
		logger.Time("start_time", execution.StartTime),
	}

	message := fmt.Sprintf("job execution timed out after %v (timeout: %v)", execution.Duration, job.Definition.Timeout)
	lm.logWithLevel(LogLevelWarn, message, fields...)
}

// OnRetry logs job retry
func (lm *LoggingMiddleware) OnRetry(ctx context.Context, job *cron.Job, execution *cron.JobExecution, attempt int) {
	if !lm.logRetry {
		return
	}

	fields := []logger.Field{
		logger.String("job_id", job.Definition.ID),
		logger.String("job_name", job.Definition.Name),
		logger.String("execution_id", execution.ID),
		logger.String("node_id", execution.NodeID),
		logger.Int("attempt", attempt),
		logger.Int("max_retries", job.Definition.MaxRetries),
		logger.Int("remaining_retries", job.Definition.MaxRetries-attempt),
	}

	// Log previous failure reason if available
	if execution.Error != "" {
		fields = append(fields, logger.String("previous_error", execution.Error))
	}

	message := fmt.Sprintf("retrying job execution (attempt %d/%d)", attempt, job.Definition.MaxRetries)
	lm.logWithLevel(LogLevelWarn, message, fields...)
}

// logWithLevel logs a message with the appropriate level
func (lm *LoggingMiddleware) logWithLevel(level LogLevel, message string, fields ...logger.Field) {
	if level < lm.logLevel {
		return
	}

	switch level {
	case LogLevelDebug:
		lm.logger.Debug(message, fields...)
	case LogLevelInfo:
		lm.logger.Info(message, fields...)
	case LogLevelWarn:
		lm.logger.Warn(message, fields...)
	case LogLevelError:
		lm.logger.Error(message, fields...)
	}
}

// sanitizeData removes sensitive information from data
func (lm *LoggingMiddleware) sanitizeData(data interface{}) interface{} {
	if data == nil {
		return nil
	}

	switch v := data.(type) {
	case map[string]interface{}:
		sanitized := make(map[string]interface{})
		for key, value := range v {
			if lm.isSensitiveKey(key) {
				sanitized[key] = "***REDACTED***"
			} else {
				sanitized[key] = lm.sanitizeData(value)
			}
		}
		return sanitized
	case []interface{}:
		sanitized := make([]interface{}, len(v))
		for i, item := range v {
			sanitized[i] = lm.sanitizeData(item)
		}
		return sanitized
	default:
		return data
	}
}

// isSensitiveKey checks if a key contains sensitive information
func (lm *LoggingMiddleware) isSensitiveKey(key string) bool {
	keyLower := strings.ToLower(key)
	for _, sensitiveKey := range lm.sensitiveKeys {
		if strings.Contains(keyLower, strings.ToLower(sensitiveKey)) {
			return true
		}
	}
	return false
}

// truncateOutput truncates output to maximum size
func (lm *LoggingMiddleware) truncateOutput(output string) string {
	if len(output) <= lm.maxOutputSize {
		return output
	}
	return output[:lm.maxOutputSize] + "...[truncated]"
}

// getPerformanceLevel categorizes job performance
func (lm *LoggingMiddleware) getPerformanceLevel(duration time.Duration) string {
	switch {
	case duration < 1*time.Second:
		return "fast"
	case duration < 10*time.Second:
		return "normal"
	case duration < 60*time.Second:
		return "slow"
	default:
		return "very_slow"
	}
}

// categorizeError categorizes error types
func (lm *LoggingMiddleware) categorizeError(err error) string {
	if forgeErr, ok := err.(*common.ForgeError); ok {
		switch forgeErr.Code {
		case common.ErrCodeTimeoutError:
			return "timeout"
		case common.ErrCodeContextCancelled:
			return "cancelled"
		case common.ErrCodeValidationError:
			return "validation"
		case common.ErrCodeServiceNotFound:
			return "service_not_found"
		case common.ErrCodeInternalError:
			return "internal"
		default:
			return "unknown"
		}
	}
	return "external"
}

// isRetryableError determines if an error is retryable
func (lm *LoggingMiddleware) isRetryableError(err error) bool {
	if forgeErr, ok := err.(*common.ForgeError); ok {
		switch forgeErr.Code {
		case common.ErrCodeTimeoutError:
			return true
		case common.ErrCodeContextCancelled:
			return false
		case common.ErrCodeValidationError:
			return false
		case common.ErrCodeServiceNotFound:
			return false
		case common.ErrCodeInternalError:
			return true
		default:
			return true
		}
	}
	return true
}

// StructuredLogger provides structured logging with additional context
type StructuredLogger struct {
	middleware  *LoggingMiddleware
	jobID       string
	executionID string
	nodeID      string
	baseFields  []logger.Field
}

// NewStructuredLogger creates a new structured logger for a job
func NewStructuredLogger(middleware *LoggingMiddleware, job *cron.Job, executionID, nodeID string) *StructuredLogger {
	baseFields := []logger.Field{
		logger.String("job_id", job.Definition.ID),
		logger.String("job_name", job.Definition.Name),
		logger.String("execution_id", executionID),
		logger.String("node_id", nodeID),
	}

	return &StructuredLogger{
		middleware:  middleware,
		jobID:       job.Definition.ID,
		executionID: executionID,
		nodeID:      nodeID,
		baseFields:  baseFields,
	}
}

// Debug logs a debug message
func (sl *StructuredLogger) Debug(message string, fields ...logger.Field) {
	allFields := append(sl.baseFields, fields...)
	sl.middleware.logWithLevel(LogLevelDebug, message, allFields...)
}

// Info logs an info message
func (sl *StructuredLogger) Info(message string, fields ...logger.Field) {
	allFields := append(sl.baseFields, fields...)
	sl.middleware.logWithLevel(LogLevelInfo, message, allFields...)
}

// Warn logs a warning message
func (sl *StructuredLogger) Warn(message string, fields ...logger.Field) {
	allFields := append(sl.baseFields, fields...)
	sl.middleware.logWithLevel(LogLevelWarn, message, allFields...)
}

// Error logs an error message
func (sl *StructuredLogger) Error(message string, fields ...logger.Field) {
	allFields := append(sl.baseFields, fields...)
	sl.middleware.logWithLevel(LogLevelError, message, allFields...)
}

// WithFields returns a new structured logger with additional fields
func (sl *StructuredLogger) WithFields(fields ...logger.Field) *StructuredLogger {
	newBaseFields := make([]logger.Field, len(sl.baseFields)+len(fields))
	copy(newBaseFields, sl.baseFields)
	copy(newBaseFields[len(sl.baseFields):], fields)

	return &StructuredLogger{
		middleware:  sl.middleware,
		jobID:       sl.jobID,
		executionID: sl.executionID,
		nodeID:      sl.nodeID,
		baseFields:  newBaseFields,
	}
}

// GetExecutionLogger returns a structured logger for the current execution
func (lm *LoggingMiddleware) GetExecutionLogger(job *cron.Job, executionID, nodeID string) *StructuredLogger {
	return NewStructuredLogger(lm, job, executionID, nodeID)
}
