package cron

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
)

// Executor handles job execution with concurrency control, timeouts, and retries.
type Executor struct {
	config   Config
	storage  Storage
	registry *JobRegistry
	logger   forge.Logger
	metrics  forge.Metrics

	// Concurrency control
	semaphore    chan struct{}
	runningJobs  map[string]*JobExecution // jobID -> execution
	runningMutex sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Node ID for distributed mode
	nodeID string
}

// NewExecutor creates a new job executor.
func NewExecutor(config Config, storage Storage, registry *JobRegistry, logger forge.Logger, metrics forge.Metrics, nodeID string) *Executor {
	ctx, cancel := context.WithCancel(context.Background())

	return &Executor{
		config:      config,
		storage:     storage,
		registry:    registry,
		logger:      logger,
		metrics:     metrics,
		nodeID:      nodeID,
		semaphore:   make(chan struct{}, config.MaxConcurrentJobs),
		runningJobs: make(map[string]*JobExecution),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Execute executes a job asynchronously.
// Returns the execution ID and any immediate errors.
func (e *Executor) Execute(ctx context.Context, job *Job) (string, error) {
	if !job.Enabled {
		return "", ErrJobDisabled
	}

	// Check if job is already running
	if e.IsJobRunning(job.ID) {
		return "", ErrJobRunning
	}

	// Create execution record
	execution := &JobExecution{
		ID:          uuid.New().String(),
		JobID:       job.ID,
		JobName:     job.Name,
		Status:      ExecutionStatusPending,
		ScheduledAt: time.Now(),
		StartedAt:   time.Now(),
		Retries:     0,
		NodeID:      e.nodeID,
	}

	// Save execution record
	if err := e.storage.SaveExecution(ctx, execution); err != nil {
		e.logger.Error("failed to save execution record",
			forge.F("job_id", job.ID),
			forge.F("execution_id", execution.ID),
			forge.F("error", err),
		)
		return "", fmt.Errorf("failed to save execution: %w", err)
	}

	// Mark job as running
	e.runningMutex.Lock()
	e.runningJobs[job.ID] = execution
	e.runningMutex.Unlock()

	// Execute asynchronously
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			// Remove from running jobs
			e.runningMutex.Lock()
			delete(e.runningJobs, job.ID)
			e.runningMutex.Unlock()
		}()

		e.executeWithRetry(job, execution)
	}()

	return execution.ID, nil
}

// executeWithRetry executes a job with retry logic.
func (e *Executor) executeWithRetry(job *Job, execution *JobExecution) {
	// Acquire semaphore slot
	select {
	case e.semaphore <- struct{}{}:
		defer func() { <-e.semaphore }()
	case <-e.ctx.Done():
		e.updateExecutionStatus(execution, ExecutionStatusCancelled, "executor shutting down")
		return
	}

	maxRetries := job.MaxRetries
	if maxRetries <= 0 {
		maxRetries = e.config.MaxRetries
	}

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff
			backoff := e.calculateBackoff(attempt)

			e.logger.Info("retrying job execution",
				forge.F("job_id", job.ID),
				forge.F("execution_id", execution.ID),
				forge.F("attempt", attempt),
				forge.F("max_retries", maxRetries),
				forge.F("backoff", backoff),
			)

			execution.Status = ExecutionStatusRetrying
			execution.Retries = attempt
			e.storage.SaveExecution(e.ctx, execution)

			// Wait for backoff
			select {
			case <-time.After(backoff):
			case <-e.ctx.Done():
				e.updateExecutionStatus(execution, ExecutionStatusCancelled, "executor shutting down")
				return
			}
		}

		// Execute the job
		err := e.executeJob(job, execution)
		if err == nil {
			// Success!
			e.updateExecutionSuccess(execution)
			return
		}

		lastErr = err

		// Check if it's a timeout or cancellation (don't retry)
		if err == ErrExecutionTimeout || err == ErrExecutionCancelled {
			return
		}
	}

	// Max retries exceeded
	e.updateExecutionStatus(execution, ExecutionStatusFailed, fmt.Sprintf("max retries exceeded: %v", lastErr))

	e.logger.Error("job execution failed after retries",
		forge.F("job_id", job.ID),
		forge.F("execution_id", execution.ID),
		forge.F("retries", maxRetries),
		forge.F("error", lastErr),
	)
}

// executeJob executes a single job attempt.
func (e *Executor) executeJob(job *Job, execution *JobExecution) error {
	start := time.Now()

	// Update status to running
	execution.Status = ExecutionStatusRunning
	execution.StartedAt = start
	e.storage.SaveExecution(e.ctx, execution)

	e.logger.Info("executing job",
		forge.F("job_id", job.ID),
		forge.F("job_name", job.Name),
		forge.F("execution_id", execution.ID),
	)

	// Determine timeout
	timeout := job.Timeout
	if timeout == 0 {
		timeout = e.config.DefaultTimeout
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(e.ctx, timeout)
	defer cancel()

	// Execute based on job type
	var err error
	if job.Handler != nil {
		// Code-based job
		err = e.executeHandler(ctx, job)
	} else if job.HandlerName != "" {
		// Named handler
		err = e.executeNamedHandler(ctx, job)
	} else if job.Command != "" {
		// Command-based job
		var output string
		output, err = e.executeCommand(ctx, job)
		execution.Output = output
	} else {
		err = ErrInvalidJobType
	}

	duration := time.Since(start)
	execution.Duration = duration

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		e.updateExecutionStatus(execution, ExecutionStatusTimeout, "execution timeout exceeded")
		return ErrExecutionTimeout
	}

	// Check for cancellation
	if ctx.Err() == context.Canceled {
		e.updateExecutionStatus(execution, ExecutionStatusCancelled, "execution cancelled")
		return ErrExecutionCancelled
	}

	// Return error to retry logic
	if err != nil {
		e.updateExecutionStatus(execution, ExecutionStatusFailed, err.Error())
		return err
	}

	return nil
}

// executeHandler executes a handler-based job with panic recovery.
func (e *Executor) executeHandler(ctx context.Context, job *Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("panic in job handler",
				forge.F("job_id", job.ID),
				forge.F("job_name", job.Name),
				forge.F("panic", r),
			)
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()

	return job.Handler(ctx, job)
}

// executeNamedHandler executes a named handler job.
func (e *Executor) executeNamedHandler(ctx context.Context, job *Job) error {
	handler, err := e.registry.Get(job.HandlerName)
	if err != nil {
		return fmt.Errorf("handler not found: %w", err)
	}

	// Execute with panic recovery
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("panic in named handler",
				forge.F("job_id", job.ID),
				forge.F("handler_name", job.HandlerName),
				forge.F("panic", r),
			)
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()

	return handler(ctx, job)
}

// executeCommand executes a command-based job.
func (e *Executor) executeCommand(ctx context.Context, job *Job) (string, error) {
	cmd := exec.CommandContext(ctx, job.Command, job.Args...)

	// Set environment variables
	if len(job.Env) > 0 {
		cmd.Env = append(cmd.Environ(), job.Env...)
	}

	// Set working directory
	if job.WorkingDir != "" {
		cmd.Dir = job.WorkingDir
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()

	// Combine output
	output := stdout.String()
	if stderr.Len() > 0 {
		if len(output) > 0 {
			output += "\n"
		}
		output += "STDERR:\n" + stderr.String()
	}

	if err != nil {
		return output, fmt.Errorf("command failed: %w", err)
	}

	return output, nil
}

// updateExecutionStatus updates the execution status and saves it.
func (e *Executor) updateExecutionStatus(execution *JobExecution, status ExecutionStatus, errorMsg string) {
	now := time.Now()
	execution.Status = status
	execution.CompletedAt = &now
	execution.Error = errorMsg

	if err := e.storage.SaveExecution(e.ctx, execution); err != nil {
		e.logger.Error("failed to update execution status",
			forge.F("execution_id", execution.ID),
			forge.F("status", status),
			forge.F("error", err),
		)
	}
}

// updateExecutionSuccess updates the execution as successful.
func (e *Executor) updateExecutionSuccess(execution *JobExecution) {
	now := time.Now()
	execution.Status = ExecutionStatusSuccess
	execution.CompletedAt = &now
	execution.Error = ""

	if err := e.storage.SaveExecution(e.ctx, execution); err != nil {
		e.logger.Error("failed to update execution success",
			forge.F("execution_id", execution.ID),
			forge.F("error", err),
		)
	}

	e.logger.Info("job execution succeeded",
		forge.F("job_id", execution.JobID),
		forge.F("execution_id", execution.ID),
		forge.F("duration", execution.Duration),
	)
}

// calculateBackoff calculates exponential backoff duration.
func (e *Executor) calculateBackoff(attempt int) time.Duration {
	backoff := float64(e.config.RetryBackoff) * math.Pow(e.config.RetryMultiplier, float64(attempt-1))
	duration := time.Duration(backoff)

	if duration > e.config.MaxRetryBackoff {
		duration = e.config.MaxRetryBackoff
	}

	return duration
}

// IsJobRunning checks if a job is currently running.
func (e *Executor) IsJobRunning(jobID string) bool {
	e.runningMutex.RLock()
	defer e.runningMutex.RUnlock()

	_, running := e.runningJobs[jobID]
	return running
}

// GetRunningJobs returns IDs of all currently running jobs.
func (e *Executor) GetRunningJobs() []string {
	e.runningMutex.RLock()
	defer e.runningMutex.RUnlock()

	jobs := make([]string, 0, len(e.runningJobs))
	for jobID := range e.runningJobs {
		jobs = append(jobs, jobID)
	}

	return jobs
}

// GetRunningCount returns the number of currently running jobs.
func (e *Executor) GetRunningCount() int {
	e.runningMutex.RLock()
	defer e.runningMutex.RUnlock()

	return len(e.runningJobs)
}

// Shutdown gracefully shuts down the executor.
// It waits for all running jobs to complete or until the context is cancelled.
func (e *Executor) Shutdown(ctx context.Context) error {
	e.logger.Info("shutting down job executor",
		forge.F("running_jobs", e.GetRunningCount()),
	)

	// Cancel the executor context
	e.cancel()

	// Wait for all jobs to complete with timeout
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		e.logger.Info("job executor shutdown complete")
		return nil
	case <-ctx.Done():
		e.logger.Warn("job executor shutdown timeout, some jobs may not have completed")
		return fmt.Errorf("shutdown timeout")
	}
}
