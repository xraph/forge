package middleware

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/cron"
	"github.com/xraph/forge/pkg/logger"
)

// RecoveryMiddleware provides panic recovery for job execution
type RecoveryMiddleware struct {
	logger                common.Logger
	metrics               common.Metrics
	captureStackTrace     bool
	captureGoroutineStack bool
	maxStackSize          int
	logPanics             bool
	logLevel              RecoveryLogLevel
	panicHandler          PanicHandler
	retryOnPanic          bool
	maxRetries            int
	retryDelay            time.Duration
	alertOnPanic          bool
	alertThreshold        int
	customErrorTransform  func(interface{}) error
}

// RecoveryLogLevel represents the logging level for recovery
type RecoveryLogLevel int

const (
	RecoveryLogLevelError RecoveryLogLevel = iota
	RecoveryLogLevelWarn
	RecoveryLogLevelInfo
	RecoveryLogLevelDebug
)

// PanicHandler defines the interface for handling panics
type PanicHandler interface {
	HandlePanic(ctx context.Context, job *cron.Job, recovered interface{}, stack []byte) error
}

// RecoveryConfig contains configuration for recovery middleware
type RecoveryConfig struct {
	CaptureStackTrace     bool                    `json:"capture_stack_trace" yaml:"capture_stack_trace"`
	CaptureGoroutineStack bool                    `json:"capture_goroutine_stack" yaml:"capture_goroutine_stack"`
	MaxStackSize          int                     `json:"max_stack_size" yaml:"max_stack_size"`
	LogPanics             bool                    `json:"log_panics" yaml:"log_panics"`
	LogLevel              RecoveryLogLevel        `json:"log_level" yaml:"log_level"`
	PanicHandler          PanicHandler            `json:"-" yaml:"-"`
	RetryOnPanic          bool                    `json:"retry_on_panic" yaml:"retry_on_panic"`
	MaxRetries            int                     `json:"max_retries" yaml:"max_retries"`
	RetryDelay            time.Duration           `json:"retry_delay" yaml:"retry_delay"`
	AlertOnPanic          bool                    `json:"alert_on_panic" yaml:"alert_on_panic"`
	AlertThreshold        int                     `json:"alert_threshold" yaml:"alert_threshold"`
	CustomErrorTransform  func(interface{}) error `json:"-" yaml:"-"`
}

// PanicInfo contains information about a panic
type PanicInfo struct {
	JobID         string                 `json:"job_id"`
	JobName       string                 `json:"job_name"`
	ExecutionID   string                 `json:"execution_id"`
	NodeID        string                 `json:"node_id"`
	Recovered     interface{}            `json:"recovered"`
	Stack         []byte                 `json:"stack"`
	Timestamp     time.Time              `json:"timestamp"`
	Goroutines    int                    `json:"goroutines"`
	MemoryStats   runtime.MemStats       `json:"memory_stats"`
	Context       map[string]interface{} `json:"context"`
	Attempt       int                    `json:"attempt"`
	Duration      time.Duration          `json:"duration"`
	PreviousPanic *PanicInfo             `json:"previous_panic,omitempty"`
}

// PanicError represents an error created from a panic
type PanicError struct {
	*common.ForgeError
	PanicInfo *PanicInfo
}

// NewRecoveryMiddleware creates a new recovery middleware
func NewRecoveryMiddleware(logger common.Logger, metrics common.Metrics, config *RecoveryConfig) *RecoveryMiddleware {
	if config == nil {
		config = &RecoveryConfig{
			CaptureStackTrace:     true,
			CaptureGoroutineStack: false,
			MaxStackSize:          4096,
			LogPanics:             true,
			LogLevel:              RecoveryLogLevelError,
			RetryOnPanic:          false,
			MaxRetries:            3,
			RetryDelay:            1 * time.Second,
			AlertOnPanic:          true,
			AlertThreshold:        3,
		}
	}

	rm := &RecoveryMiddleware{
		logger:                logger,
		metrics:               metrics,
		captureStackTrace:     config.CaptureStackTrace,
		captureGoroutineStack: config.CaptureGoroutineStack,
		maxStackSize:          config.MaxStackSize,
		logPanics:             config.LogPanics,
		logLevel:              config.LogLevel,
		panicHandler:          config.PanicHandler,
		retryOnPanic:          config.RetryOnPanic,
		maxRetries:            config.MaxRetries,
		retryDelay:            config.RetryDelay,
		alertOnPanic:          config.AlertOnPanic,
		alertThreshold:        config.AlertThreshold,
		customErrorTransform:  config.CustomErrorTransform,
	}

	// Set default panic handler if none provided
	if rm.panicHandler == nil {
		rm.panicHandler = &DefaultPanicHandler{
			logger:  logger,
			metrics: metrics,
		}
	}

	return rm
}

// Handle handles job execution with panic recovery
func (rm *RecoveryMiddleware) Handle(ctx context.Context, job *cron.Job, next func(ctx context.Context, job *cron.Job) error) (err error) {
	start := time.Now()
	var panicInfo *PanicInfo
	attempt := 1

	// Get current attempt from execution context
	if job.CurrentExecution != nil {
		attempt = job.CurrentExecution.Attempt
	}

	// Recovery function
	defer func() {
		if recovered := recover(); recovered != nil {
			duration := time.Since(start)

			// Create panic info
			panicInfo = rm.createPanicInfo(job, recovered, duration, attempt)

			// Record panic metrics
			rm.recordPanicMetrics(job, panicInfo)

			// Log panic if enabled
			if rm.logPanics {
				rm.logPanic(job, panicInfo)
			}

			// Handle panic
			if rm.panicHandler != nil {
				if handlerErr := rm.panicHandler.HandlePanic(ctx, job, recovered, panicInfo.Stack); handlerErr != nil {
					if rm.logger != nil {
						rm.logger.Error("panic handler failed",
							logger.String("job_id", job.Definition.ID),
							logger.Error(handlerErr),
						)
					}
				}
			}

			// Transform panic to error
			err = rm.transformPanicToError(recovered, panicInfo)

			// Check if we should retry
			if rm.retryOnPanic && rm.shouldRetry(job, attempt) {
				if rm.logger != nil {
					rm.logger.Info("retrying job after panic",
						logger.String("job_id", job.Definition.ID),
						logger.Int("attempt", attempt),
						logger.Int("max_retries", rm.maxRetries),
					)
				}

				// Wait before retry
				if rm.retryDelay > 0 {
					select {
					case <-time.After(rm.retryDelay):
					case <-ctx.Done():
						err = ctx.Err()
						return
					}
				}

				// Retry the execution
				retryErr := rm.retryExecution(ctx, job, next, attempt+1, panicInfo)
				if retryErr != nil {
					err = retryErr
				}
			}
		}
	}()

	// Execute the job
	err = next(ctx, job)
	return err
}

// createPanicInfo creates detailed panic information
func (rm *RecoveryMiddleware) createPanicInfo(job *cron.Job, recovered interface{}, duration time.Duration, attempt int) *PanicInfo {
	panicInfo := &PanicInfo{
		JobID:      job.Definition.ID,
		JobName:    job.Definition.Name,
		Recovered:  recovered,
		Timestamp:  time.Now(),
		Goroutines: runtime.NumGoroutine(),
		Context:    make(map[string]interface{}),
		Attempt:    attempt,
		Duration:   duration,
	}

	// Add execution context
	if job.CurrentExecution != nil {
		panicInfo.ExecutionID = job.CurrentExecution.ID
		panicInfo.NodeID = job.CurrentExecution.NodeID
	}

	// Capture stack trace
	if rm.captureStackTrace {
		if rm.captureGoroutineStack {
			panicInfo.Stack = debug.Stack()
		} else {
			stack := make([]byte, rm.maxStackSize)
			length := runtime.Stack(stack, false)
			panicInfo.Stack = stack[:length]
		}
	}

	// Capture memory stats
	runtime.ReadMemStats(&panicInfo.MemoryStats)

	// Add job context
	panicInfo.Context["job_enabled"] = job.Definition.Enabled
	panicInfo.Context["job_singleton"] = job.Definition.Singleton
	panicInfo.Context["job_distributed"] = job.Definition.Distributed
	panicInfo.Context["job_priority"] = job.Definition.Priority
	panicInfo.Context["job_max_retries"] = job.Definition.MaxRetries
	panicInfo.Context["job_timeout"] = job.Definition.Timeout
	panicInfo.Context["job_run_count"] = job.RunCount
	panicInfo.Context["job_success_count"] = job.SuccessCount
	panicInfo.Context["job_failure_count"] = job.FailureCount
	panicInfo.Context["job_status"] = job.Status

	// Add job tags
	if len(job.Definition.Tags) > 0 {
		panicInfo.Context["job_tags"] = job.Definition.Tags
	}

	// Add dependencies
	if len(job.Definition.Dependencies) > 0 {
		panicInfo.Context["job_dependencies"] = job.Definition.Dependencies
	}

	return panicInfo
}

// recordPanicMetrics records panic-related metrics
func (rm *RecoveryMiddleware) recordPanicMetrics(job *cron.Job, panicInfo *PanicInfo) {
	if rm.metrics == nil {
		return
	}

	tags := []string{
		"job_id", job.Definition.ID,
		"job_name", job.Definition.Name,
		"attempt", fmt.Sprintf("%d", panicInfo.Attempt),
	}

	if panicInfo.NodeID != "" {
		tags = append(tags, "node_id", panicInfo.NodeID)
	}

	// Record panic counter
	rm.metrics.Counter("forge.cron.job.panics", tags...).Inc()

	// Record panic by type
	panicType := fmt.Sprintf("%T", panicInfo.Recovered)
	panicTypeTags := append(tags, "panic_type", panicType)
	rm.metrics.Counter("forge.cron.job.panics.by_type", panicTypeTags...).Inc()

	// Record panic duration
	rm.metrics.Histogram("forge.cron.job.panic.duration", tags...).Observe(panicInfo.Duration.Seconds())

	// Record goroutines at panic
	rm.metrics.Gauge("forge.cron.job.panic.goroutines", tags...).Set(float64(panicInfo.Goroutines))

	// Record memory stats at panic
	rm.metrics.Gauge("forge.cron.job.panic.memory.alloc", tags...).Set(float64(panicInfo.MemoryStats.Alloc))
	rm.metrics.Gauge("forge.cron.job.panic.memory.sys", tags...).Set(float64(panicInfo.MemoryStats.Sys))
	rm.metrics.Gauge("forge.cron.job.panic.memory.heap_alloc", tags...).Set(float64(panicInfo.MemoryStats.HeapAlloc))

	// Record stack trace size
	if panicInfo.Stack != nil {
		rm.metrics.Gauge("forge.cron.job.panic.stack_size", tags...).Set(float64(len(panicInfo.Stack)))
	}
}

// logPanic logs panic information
func (rm *RecoveryMiddleware) logPanic(job *cron.Job, panicInfo *PanicInfo) {
	if rm.logger == nil {
		return
	}

	fields := []logger.Field{
		logger.String("job_id", panicInfo.JobID),
		logger.String("job_name", panicInfo.JobName),
		logger.String("execution_id", panicInfo.ExecutionID),
		logger.String("node_id", panicInfo.NodeID),
		logger.Any("recovered", panicInfo.Recovered),
		logger.Time("timestamp", panicInfo.Timestamp),
		logger.Int("goroutines", panicInfo.Goroutines),
		logger.Int("attempt", panicInfo.Attempt),
		logger.Duration("duration", panicInfo.Duration),
		logger.Uint64("memory_alloc", panicInfo.MemoryStats.Alloc),
		logger.Uint64("memory_sys", panicInfo.MemoryStats.Sys),
		logger.Uint64("memory_heap_alloc", panicInfo.MemoryStats.HeapAlloc),
		logger.Uint32("memory_gc_cycles", panicInfo.MemoryStats.NumGC),
	}

	// Add stack trace if available
	if panicInfo.Stack != nil {
		fields = append(fields, logger.String("stack_trace", string(panicInfo.Stack)))
	}

	// Add context fields
	for key, value := range panicInfo.Context {
		fields = append(fields, logger.Any(fmt.Sprintf("context_%s", key), value))
	}

	message := fmt.Sprintf("job panic recovered: %v", panicInfo.Recovered)

	// Log with appropriate level
	switch rm.logLevel {
	case RecoveryLogLevelError:
		rm.logger.Error(message, fields...)
	case RecoveryLogLevelWarn:
		rm.logger.Warn(message, fields...)
	case RecoveryLogLevelInfo:
		rm.logger.Info(message, fields...)
	case RecoveryLogLevelDebug:
		rm.logger.Debug(message, fields...)
	}
}

// transformPanicToError transforms a panic into an error
func (rm *RecoveryMiddleware) transformPanicToError(recovered interface{}, panicInfo *PanicInfo) error {
	// Use custom error transformer if provided
	if rm.customErrorTransform != nil {
		return rm.customErrorTransform(recovered)
	}

	// Default transformation
	var message string
	switch v := recovered.(type) {
	case error:
		message = v.Error()
	case string:
		message = v
	default:
		message = fmt.Sprintf("panic: %v", v)
	}

	// Create ForgeError with panic context
	forgeErr := common.NewForgeError(common.ErrCodeInternalError, message, nil)
	forgeErr.WithContext("panic_type", fmt.Sprintf("%T", recovered))
	forgeErr.WithContext("panic_value", recovered)
	forgeErr.WithContext("job_id", panicInfo.JobID)
	forgeErr.WithContext("job_name", panicInfo.JobName)
	forgeErr.WithContext("execution_id", panicInfo.ExecutionID)
	forgeErr.WithContext("node_id", panicInfo.NodeID)
	forgeErr.WithContext("attempt", panicInfo.Attempt)
	forgeErr.WithContext("duration", panicInfo.Duration)
	forgeErr.WithContext("goroutines", panicInfo.Goroutines)

	// Add stack trace if available
	if panicInfo.Stack != nil {
		forgeErr.WithContext("stack_trace", string(panicInfo.Stack))
	}

	// Create panic error
	panicError := &PanicError{
		ForgeError: forgeErr,
		PanicInfo:  panicInfo,
	}

	return panicError
}

// shouldRetry determines if a job should be retried after panic
func (rm *RecoveryMiddleware) shouldRetry(job *cron.Job, attempt int) bool {
	if !rm.retryOnPanic {
		return false
	}

	if attempt >= rm.maxRetries {
		return false
	}

	// Check if job allows retries
	if job.Definition.MaxRetries > 0 && attempt >= job.Definition.MaxRetries {
		return false
	}

	return true
}

// retryExecution retries job execution after panic
func (rm *RecoveryMiddleware) retryExecution(ctx context.Context, job *cron.Job, next func(ctx context.Context, job *cron.Job) error, attempt int, previousPanic *PanicInfo) error {
	// Create new panic info for retry
	start := time.Now()
	var retryPanicInfo *PanicInfo

	// Recovery function for retry
	defer func() {
		if recovered := recover(); recovered != nil {
			duration := time.Since(start)

			// Create panic info for retry
			retryPanicInfo = rm.createPanicInfo(job, recovered, duration, attempt)
			retryPanicInfo.PreviousPanic = previousPanic

			// Record retry panic metrics
			rm.recordRetryPanicMetrics(job, retryPanicInfo)

			// Log retry panic
			if rm.logPanics {
				rm.logRetryPanic(job, retryPanicInfo)
			}
		}
	}()

	// Execute retry
	return next(ctx, job)
}

// recordRetryPanicMetrics records metrics for retry panics
func (rm *RecoveryMiddleware) recordRetryPanicMetrics(job *cron.Job, panicInfo *PanicInfo) {
	if rm.metrics == nil {
		return
	}

	tags := []string{
		"job_id", job.Definition.ID,
		"job_name", job.Definition.Name,
		"attempt", fmt.Sprintf("%d", panicInfo.Attempt),
		"is_retry", "true",
	}

	if panicInfo.NodeID != "" {
		tags = append(tags, "node_id", panicInfo.NodeID)
	}

	// Record retry panic counter
	rm.metrics.Counter("forge.cron.job.panics.retry", tags...).Inc()

	// Record consecutive panics
	if panicInfo.PreviousPanic != nil {
		rm.metrics.Counter("forge.cron.job.panics.consecutive", tags...).Inc()
	}
}

// logRetryPanic logs retry panic information
func (rm *RecoveryMiddleware) logRetryPanic(job *cron.Job, panicInfo *PanicInfo) {
	if rm.logger == nil {
		return
	}

	fields := []logger.Field{
		logger.String("job_id", panicInfo.JobID),
		logger.String("job_name", panicInfo.JobName),
		logger.String("execution_id", panicInfo.ExecutionID),
		logger.String("node_id", panicInfo.NodeID),
		logger.Any("recovered", panicInfo.Recovered),
		logger.Int("attempt", panicInfo.Attempt),
		logger.Bool("is_retry", true),
		logger.Duration("duration", panicInfo.Duration),
	}

	// Add previous panic info
	if panicInfo.PreviousPanic != nil {
		fields = append(fields, logger.Any("previous_panic", panicInfo.PreviousPanic.Recovered))
	}

	message := fmt.Sprintf("job retry panic recovered: %v", panicInfo.Recovered)
	rm.logger.Error(message, fields...)
}

// OnPanic handles panic events
func (rm *RecoveryMiddleware) OnPanic(ctx context.Context, job *cron.Job, execution *cron.JobExecution, recovered interface{}) {
	// This method can be called by external components to handle panics
	panicInfo := &PanicInfo{
		JobID:       job.Definition.ID,
		JobName:     job.Definition.Name,
		ExecutionID: execution.ID,
		NodeID:      execution.NodeID,
		Recovered:   recovered,
		Timestamp:   time.Now(),
		Goroutines:  runtime.NumGoroutine(),
		Context:     make(map[string]interface{}),
		Attempt:     execution.Attempt,
		Duration:    execution.Duration,
	}

	// Capture stack trace
	if rm.captureStackTrace {
		panicInfo.Stack = debug.Stack()
	}

	// Record metrics and log
	rm.recordPanicMetrics(job, panicInfo)
	if rm.logPanics {
		rm.logPanic(job, panicInfo)
	}

	// Handle panic
	if rm.panicHandler != nil {
		rm.panicHandler.HandlePanic(ctx, job, recovered, panicInfo.Stack)
	}
}

// DefaultPanicHandler provides default panic handling
type DefaultPanicHandler struct {
	logger  common.Logger
	metrics common.Metrics
}

// HandlePanic handles a panic with default behavior
func (dph *DefaultPanicHandler) HandlePanic(ctx context.Context, job *cron.Job, recovered interface{}, stack []byte) error {
	// Log the panic
	if dph.logger != nil {
		dph.logger.Error("default panic handler invoked",
			logger.String("job_id", job.Definition.ID),
			logger.String("job_name", job.Definition.Name),
			logger.Any("recovered", recovered),
			logger.String("stack", string(stack)),
		)
	}

	// Record metrics
	if dph.metrics != nil {
		tags := []string{
			"job_id", job.Definition.ID,
			"job_name", job.Definition.Name,
			"handler", "default",
		}
		dph.metrics.Counter("forge.cron.job.panics.handled", tags...).Inc()
	}

	return nil
}

// AlertPanicHandler sends alerts for panics
type AlertPanicHandler struct {
	logger       common.Logger
	metrics      common.Metrics
	alertManager interface {
		SendAlert(ctx context.Context, alert interface{}) error
	}
	threshold   int
	panicCounts map[string]int
}

// NewAlertPanicHandler creates a new alert panic handler
func NewAlertPanicHandler(logger common.Logger, metrics common.Metrics, alertManager interface {
	SendAlert(ctx context.Context, alert interface{}) error
}, threshold int) *AlertPanicHandler {
	return &AlertPanicHandler{
		logger:       logger,
		metrics:      metrics,
		alertManager: alertManager,
		threshold:    threshold,
		panicCounts:  make(map[string]int),
	}
}

// HandlePanic handles a panic with alerting
func (aph *AlertPanicHandler) HandlePanic(ctx context.Context, job *cron.Job, recovered interface{}, stack []byte) error {
	jobID := job.Definition.ID

	// Increment panic count
	aph.panicCounts[jobID]++

	// Send alert if threshold reached
	if aph.panicCounts[jobID] >= aph.threshold {
		alert := map[string]interface{}{
			"type":        "job_panic",
			"job_id":      jobID,
			"job_name":    job.Definition.Name,
			"panic_count": aph.panicCounts[jobID],
			"threshold":   aph.threshold,
			"recovered":   recovered,
			"stack_trace": string(stack),
			"timestamp":   time.Now(),
		}

		if err := aph.alertManager.SendAlert(ctx, alert); err != nil {
			if aph.logger != nil {
				aph.logger.Error("failed to send panic alert",
					logger.String("job_id", jobID),
					logger.Error(err),
				)
			}
			return err
		}

		// Reset count after alert
		aph.panicCounts[jobID] = 0
	}

	return nil
}

// GetPanicInfo extracts panic information from a PanicError
func GetPanicInfo(err error) *PanicInfo {
	if panicErr, ok := err.(*PanicError); ok {
		return panicErr.PanicInfo
	}
	return nil
}

// IsPanicError checks if an error is a panic error
func IsPanicError(err error) bool {
	_, ok := err.(*PanicError)
	return ok
}

// RecoveryStats provides statistics about panic recovery
type RecoveryStats struct {
	TotalPanics     int64            `json:"total_panics"`
	PanicsByJob     map[string]int64 `json:"panics_by_job"`
	PanicsByType    map[string]int64 `json:"panics_by_type"`
	RecoveredPanics int64            `json:"recovered_panics"`
	RetryPanics     int64            `json:"retry_panics"`
	AlertsSent      int64            `json:"alerts_sent"`
	LastPanic       *PanicInfo       `json:"last_panic"`
	LastUpdate      time.Time        `json:"last_update"`
}

// GetRecoveryStats returns recovery statistics
func (rm *RecoveryMiddleware) GetRecoveryStats() *RecoveryStats {
	// This would typically be implemented with persistent storage
	// For now, return empty stats
	return &RecoveryStats{
		LastUpdate: time.Now(),
	}
}

// HealthCheckRecovery provides health check for recovery middleware
func (rm *RecoveryMiddleware) HealthCheckRecovery() error {
	// Check if recovery middleware is functioning properly
	// This could include checking alert systems, panic handlers, etc.
	return nil
}

// SetPanicHandler sets a custom panic handler
func (rm *RecoveryMiddleware) SetPanicHandler(handler PanicHandler) {
	rm.panicHandler = handler
}

// GetPanicHandler returns the current panic handler
func (rm *RecoveryMiddleware) GetPanicHandler() PanicHandler {
	return rm.panicHandler
}

// EnableRetryOnPanic enables retry on panic
func (rm *RecoveryMiddleware) EnableRetryOnPanic(maxRetries int, delay time.Duration) {
	rm.retryOnPanic = true
	rm.maxRetries = maxRetries
	rm.retryDelay = delay
}

// DisableRetryOnPanic disables retry on panic
func (rm *RecoveryMiddleware) DisableRetryOnPanic() {
	rm.retryOnPanic = false
}

// SetLogLevel sets the logging level for recovery
func (rm *RecoveryMiddleware) SetLogLevel(level RecoveryLogLevel) {
	rm.logLevel = level
}
