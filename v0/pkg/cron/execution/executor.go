package execution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	cron "github.com/xraph/forge/pkg/cron/core"
	"github.com/xraph/forge/pkg/logger"
)

// Executor manages job execution
type Executor struct {
	config         *Config
	workerPool     *WorkerPool
	retryManager   *RetryManager
	timeoutManager *TimeoutManager

	// Execution state
	runningJobs map[string]*ExecutionContext
	mu          sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// Config contains executor configuration
type Config struct {
	MaxConcurrentJobs int           `json:"max_concurrent_jobs" yaml:"max_concurrent_jobs"`
	DefaultTimeout    time.Duration `json:"default_timeout" yaml:"default_timeout"`
	WorkerPoolSize    int           `json:"worker_pool_size" yaml:"worker_pool_size"`
	RetryConfig       *RetryConfig  `json:"retry" yaml:"retry"`
	EnableMetrics     bool          `json:"enable_metrics" yaml:"enable_metrics"`
	ExecutionTimeout  time.Duration `json:"execution_timeout" yaml:"execution_timeout"`
}

// ExecutionContext represents the context of a job execution
type ExecutionContext struct {
	Job       *cron.Job
	Execution *cron.JobExecution
	Handler   cron.JobHandler
	Context   context.Context
	Cancel    context.CancelFunc
	StartTime time.Time
	Worker    *Worker
	Result    *ExecutionResult
}

// ExecutionResult represents the result of a job execution
type ExecutionResult struct {
	Success  bool
	Output   string
	Error    error
	Duration time.Duration
	Metadata map[string]interface{}
}

// DefaultConfig returns default executor configuration
func DefaultConfig() *Config {
	return &Config{
		MaxConcurrentJobs: 10,
		DefaultTimeout:    30 * time.Minute,
		WorkerPoolSize:    5,
		RetryConfig:       DefaultRetryConfig(),
		EnableMetrics:     true,
		ExecutionTimeout:  1 * time.Hour,
	}
}

// NewExecutor creates a new job executor
func NewExecutor(config *Config, logger common.Logger, metrics common.Metrics) (*Executor, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := validateConfig(config); err != nil {
		return nil, err
	}

	// Create worker pool
	workerPool := NewWorkerPool(config.WorkerPoolSize, logger, metrics)

	// Create retry manager
	retryManager := NewRetryManager(config.RetryConfig, logger, metrics)

	// Create timeout manager
	timeoutManager := NewTimeoutManager(config.DefaultTimeout, logger, metrics)

	executor := &Executor{
		config:         config,
		workerPool:     workerPool,
		retryManager:   retryManager,
		timeoutManager: timeoutManager,
		runningJobs:    make(map[string]*ExecutionContext),
		stopChannel:    make(chan struct{}),
		logger:         logger,
		metrics:        metrics,
	}

	return executor, nil
}

// Start starts the executor
func (e *Executor) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return common.ErrLifecycleError("start", fmt.Errorf("executor already started"))
	}

	// Start worker pool
	if err := e.workerPool.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("executor", err)
	}

	// Start retry manager
	if err := e.retryManager.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("executor", err)
	}

	// Start timeout manager
	if err := e.timeoutManager.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("executor", err)
	}

	// Start monitoring
	e.wg.Add(1)
	go e.monitorExecutions(ctx)

	e.started = true

	if e.logger != nil {
		e.logger.Info("executor started",
			logger.Int("max_concurrent_jobs", e.config.MaxConcurrentJobs),
			logger.Int("worker_pool_size", e.config.WorkerPoolSize),
		)
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.cron.executor_started").Inc()
	}

	return nil
}

// Stop stops the executor
func (e *Executor) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("executor not started"))
	}

	if e.logger != nil {
		e.logger.Info("stopping executor")
	}

	// Signal stop
	close(e.stopChannel)

	// Cancel all running jobs
	for _, execCtx := range e.runningJobs {
		if execCtx.Cancel != nil {
			execCtx.Cancel()
		}
	}

	// Wait for monitoring to finish
	e.wg.Wait()

	// Stop components
	if err := e.timeoutManager.Stop(ctx); err != nil {
		if e.logger != nil {
			e.logger.Error("failed to stop timeout manager", logger.Error(err))
		}
	}

	if err := e.retryManager.Stop(ctx); err != nil {
		if e.logger != nil {
			e.logger.Error("failed to stop retry manager", logger.Error(err))
		}
	}

	if err := e.workerPool.Stop(ctx); err != nil {
		if e.logger != nil {
			e.logger.Error("failed to stop worker pool", logger.Error(err))
		}
	}

	e.started = false

	if e.logger != nil {
		e.logger.Info("executor stopped")
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.cron.executor_stopped").Inc()
	}

	return nil
}

// Execute executes a job
func (e *Executor) Execute(ctx context.Context, job *cron.Job, execution *cron.JobExecution, handler cron.JobHandler) error {
	if !e.started {
		return common.ErrLifecycleError("execute", fmt.Errorf("executor not started"))
	}

	// Check if we can accept more jobs
	e.mu.RLock()
	if len(e.runningJobs) >= e.config.MaxConcurrentJobs {
		e.mu.RUnlock()
		return common.ErrContainerError("execute", fmt.Errorf("max concurrent jobs reached"))
	}
	e.mu.RUnlock()

	// Create execution context
	execCtx, cancel := context.WithCancel(ctx)
	executionContext := &ExecutionContext{
		Job:       job,
		Execution: execution,
		Handler:   handler,
		Context:   execCtx,
		Cancel:    cancel,
		StartTime: time.Now(),
	}

	// Add to running jobs
	e.mu.Lock()
	e.runningJobs[execution.ID] = executionContext
	e.mu.Unlock()

	// Submit to worker pool
	if err := e.workerPool.Submit(executionContext); err != nil {
		// Remove from running jobs
		e.mu.Lock()
		delete(e.runningJobs, execution.ID)
		e.mu.Unlock()

		cancel()
		return common.ErrContainerError("execute", err)
	}

	if e.logger != nil {
		e.logger.Info("job execution submitted",
			logger.String("job_id", job.Definition.ID),
			logger.String("execution_id", execution.ID),
		)
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.cron.executions_submitted").Inc()
		e.metrics.Gauge("forge.cron.running_jobs").Set(float64(len(e.runningJobs)))
	}

	return nil
}

// Cancel cancels a job execution
func (e *Executor) Cancel(ctx context.Context, executionID string) error {
	e.mu.RLock()
	executionContext, exists := e.runningJobs[executionID]
	e.mu.RUnlock()

	if !exists {
		return common.ErrServiceNotFound(executionID)
	}

	// Cancel the execution
	if executionContext.Cancel != nil {
		executionContext.Cancel()
	}

	// Update execution status
	executionContext.Execution.Status = cron.ExecutionStatusCancelled

	if e.logger != nil {
		e.logger.Info("job execution cancelled",
			logger.String("execution_id", executionID),
		)
	}

	if e.metrics != nil {
		e.metrics.Counter("forge.cron.executions_cancelled").Inc()
	}

	return nil
}

// GetRunningJobs returns all running jobs
func (e *Executor) GetRunningJobs() []*ExecutionContext {
	e.mu.RLock()
	defer e.mu.RUnlock()

	jobs := make([]*ExecutionContext, 0, len(e.runningJobs))
	for _, execCtx := range e.runningJobs {
		jobs = append(jobs, execCtx)
	}

	return jobs
}

// GetExecutionContext returns the execution context for a job
func (e *Executor) GetExecutionContext(executionID string) (*ExecutionContext, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	executionContext, exists := e.runningJobs[executionID]
	if !exists {
		return nil, common.ErrServiceNotFound(executionID)
	}

	return executionContext, nil
}

// HealthCheck performs a health check
func (e *Executor) HealthCheck(ctx context.Context) error {
	if !e.started {
		return common.ErrHealthCheckFailed("executor", fmt.Errorf("executor not started"))
	}

	// Check worker pool health
	if err := e.workerPool.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("executor", err)
	}

	// Check retry manager health
	if err := e.retryManager.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("executor", err)
	}

	// Check timeout manager health
	if err := e.timeoutManager.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("executor", err)
	}

	// Check for stuck jobs
	e.mu.RLock()
	stuckJobs := 0
	stuckThreshold := time.Now().Add(-e.config.ExecutionTimeout)

	for _, execCtx := range e.runningJobs {
		if execCtx.StartTime.Before(stuckThreshold) {
			stuckJobs++
		}
	}
	e.mu.RUnlock()

	if stuckJobs > 0 {
		return common.ErrHealthCheckFailed("executor", fmt.Errorf("found %d stuck jobs", stuckJobs))
	}

	return nil
}

// GetStats returns executor statistics
func (e *Executor) GetStats() ExecutorStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := ExecutorStats{
		MaxConcurrentJobs: e.config.MaxConcurrentJobs,
		RunningJobs:       len(e.runningJobs),
		WorkerPoolSize:    e.config.WorkerPoolSize,
		Started:           e.started,
		LastUpdate:        time.Now(),
	}

	// Get worker pool stats
	if workerStats := e.workerPool.GetStats(); workerStats != nil {
		stats.WorkerStats = *workerStats
	}

	// Get retry manager stats
	if retryStats := e.retryManager.GetStats(); retryStats != nil {
		stats.RetryStats = *retryStats
	}

	return stats
}

// monitorExecutions monitors running executions
func (e *Executor) monitorExecutions(ctx context.Context) {
	defer e.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChannel:
			return
		case <-ticker.C:
			e.checkExecutions()
		case <-ctx.Done():
			return
		}
	}
}

// checkExecutions checks for stuck or long-running executions
func (e *Executor) checkExecutions() {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := time.Now()
	stuckThreshold := now.Add(-e.config.ExecutionTimeout)

	for executionID, execCtx := range e.runningJobs {
		// Check for stuck jobs
		if execCtx.StartTime.Before(stuckThreshold) {
			if e.logger != nil {
				e.logger.Warn("job execution stuck",
					logger.String("execution_id", executionID),
					logger.String("job_id", execCtx.Job.Definition.ID),
					logger.Duration("running_time", now.Sub(execCtx.StartTime)),
				)
			}

			// Cancel stuck job
			if execCtx.Cancel != nil {
				execCtx.Cancel()
			}

			if e.metrics != nil {
				e.metrics.Counter("forge.cron.executions_stuck").Inc()
			}
		}
	}
}

// OnExecutionComplete handles completion of job execution
func (e *Executor) OnExecutionComplete(executionID string, result *ExecutionResult) {
	e.mu.Lock()
	executionContext, exists := e.runningJobs[executionID]
	if exists {
		executionContext.Result = result
		delete(e.runningJobs, executionID)
	}
	e.mu.Unlock()

	if !exists {
		return
	}

	// Update execution
	execution := executionContext.Execution
	execution.Duration = result.Duration
	execution.Output = result.Output

	if result.Success {
		execution.Status = cron.ExecutionStatusSuccess

		// Call handler success callback
		if handler := executionContext.Handler; handler != nil {
			handler.OnSuccess(executionContext.Context, executionContext.Job, execution)
		}
	} else {
		execution.Status = cron.ExecutionStatusFailed
		if result.Error != nil {
			execution.Error = result.Error.Error()
		}

		// Call handler failure callback
		if handler := executionContext.Handler; handler != nil {
			handler.OnFailure(executionContext.Context, executionContext.Job, execution, result.Error)
		}

		// Check if retry is needed
		if executionContext.Job.CanRetry() {
			e.retryManager.ScheduleRetry(executionContext.Job, execution, result.Error)
		}
	}

	if e.logger != nil {
		if result.Success {
			e.logger.Info("job execution completed successfully",
				logger.String("job_id", executionContext.Job.Definition.ID),
				logger.String("execution_id", executionID),
				logger.Duration("duration", result.Duration),
			)
		} else {
			e.logger.Error("job execution failed",
				logger.String("job_id", executionContext.Job.Definition.ID),
				logger.String("execution_id", executionID),
				logger.Duration("duration", result.Duration),
				logger.Error(result.Error),
			)
		}
	}

	if e.metrics != nil {
		if result.Success {
			e.metrics.Counter("forge.cron.executions_success").Inc()
		} else {
			e.metrics.Counter("forge.cron.executions_failed").Inc()
		}
		e.metrics.Histogram("forge.cron.execution_duration").Observe(result.Duration.Seconds())
		e.metrics.Gauge("forge.cron.running_jobs").Set(float64(len(e.runningJobs)))
	}
}

// validateConfig validates executor configuration
func validateConfig(config *Config) error {
	if config.MaxConcurrentJobs <= 0 {
		return common.ErrValidationError("max_concurrent_jobs", fmt.Errorf("max concurrent jobs must be positive"))
	}

	if config.WorkerPoolSize <= 0 {
		return common.ErrValidationError("worker_pool_size", fmt.Errorf("worker pool size must be positive"))
	}

	if config.DefaultTimeout <= 0 {
		return common.ErrValidationError("default_timeout", fmt.Errorf("default timeout must be positive"))
	}

	if config.ExecutionTimeout <= 0 {
		return common.ErrValidationError("execution_timeout", fmt.Errorf("execution timeout must be positive"))
	}

	if config.RetryConfig != nil {
		if err := validateRetryConfig(config.RetryConfig); err != nil {
			return err
		}
	}

	return nil
}

// ExecutorStats contains executor statistics
type ExecutorStats struct {
	MaxConcurrentJobs int         `json:"max_concurrent_jobs"`
	RunningJobs       int         `json:"running_jobs"`
	WorkerPoolSize    int         `json:"worker_pool_size"`
	Started           bool        `json:"started"`
	WorkerStats       WorkerStats `json:"worker_stats"`
	RetryStats        RetryStats  `json:"retry_stats"`
	LastUpdate        time.Time   `json:"last_update"`
}
