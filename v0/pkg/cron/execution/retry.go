package execution

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	cron "github.com/xraph/forge/pkg/cron/core"
	"github.com/xraph/forge/pkg/logger"
)

// RetryManager manages job retries with various backoff strategies
type RetryManager struct {
	config          *RetryConfig
	retryQueue      chan *RetryTask
	deadLetterQueue chan *RetryTask
	strategies      map[string]BackoffStrategy

	// Retry state
	activeRetries map[string]*RetryContext
	retryStats    map[string]*RetryJobStats
	mu            sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// RetryConfig contains retry configuration
type RetryConfig struct {
	MaxRetries            int                    `json:"max_retries" yaml:"max_retries"`
	DefaultStrategy       string                 `json:"default_strategy" yaml:"default_strategy"`
	InitialBackoff        time.Duration          `json:"initial_backoff" yaml:"initial_backoff"`
	MaxBackoff            time.Duration          `json:"max_backoff" yaml:"max_backoff"`
	BackoffMultiplier     float64                `json:"backoff_multiplier" yaml:"backoff_multiplier"`
	Jitter                bool                   `json:"jitter" yaml:"jitter"`
	JitterRange           float64                `json:"jitter_range" yaml:"jitter_range"`
	WorkerCount           int                    `json:"worker_count" yaml:"worker_count"`
	QueueSize             int                    `json:"queue_size" yaml:"queue_size"`
	DeadLetterQueueSize   int                    `json:"dead_letter_queue_size" yaml:"dead_letter_queue_size"`
	RetryableErrors       []string               `json:"retryable_errors" yaml:"retryable_errors"`
	NonRetryableErrors    []string               `json:"non_retryable_errors" yaml:"non_retryable_errors"`
	EnableDeadLetterQueue bool                   `json:"enable_dead_letter_queue" yaml:"enable_dead_letter_queue"`
	RetryTimeout          time.Duration          `json:"retry_timeout" yaml:"retry_timeout"`
	Strategies            map[string]interface{} `json:"strategies" yaml:"strategies"`
}

// RetryTask represents a job that needs to be retried
type RetryTask struct {
	Job         *cron.Job
	Execution   *cron.JobExecution
	Error       error
	Attempt     int
	MaxAttempts int
	Strategy    string
	ScheduledAt time.Time
	Context     map[string]interface{}
	CreatedAt   time.Time
}

// RetryContext tracks retry state for a job
type RetryContext struct {
	JobID       string
	Attempts    int
	MaxAttempts int
	Strategy    BackoffStrategy
	LastAttempt time.Time
	NextAttempt time.Time
	Errors      []error
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RetryJobStats tracks retry statistics for a job
type RetryJobStats struct {
	JobID             string        `json:"job_id"`
	TotalRetries      int64         `json:"total_retries"`
	SuccessfulRetries int64         `json:"successful_retries"`
	FailedRetries     int64         `json:"failed_retries"`
	AverageBackoff    time.Duration `json:"average_backoff"`
	MaxBackoff        time.Duration `json:"max_backoff"`
	MinBackoff        time.Duration `json:"min_backoff"`
	LastRetry         time.Time     `json:"last_retry"`
	Strategy          string        `json:"strategy"`
}

// BackoffStrategy defines the interface for retry backoff strategies
type BackoffStrategy interface {
	GetDelay(attempt int, lastError error) time.Duration
	ShouldRetry(attempt int, maxAttempts int, err error) bool
	Name() string
	Configure(config map[string]interface{}) error
}

// ExponentialBackoff implements exponential backoff strategy
type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Jitter       bool
	JitterRange  float64
}

// LinearBackoff implements linear backoff strategy
type LinearBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Increment    time.Duration
	Jitter       bool
	JitterRange  float64
}

// FixedBackoff implements fixed backoff strategy
type FixedBackoff struct {
	Delay       time.Duration
	Jitter      bool
	JitterRange float64
}

// FibonacciBackoff implements Fibonacci backoff strategy
type FibonacciBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Jitter       bool
	JitterRange  float64
}

// CustomBackoff allows for custom backoff logic
type CustomBackoff struct {
	DelayFunc       func(attempt int, lastError error) time.Duration
	ShouldRetryFunc func(attempt int, maxAttempts int, err error) bool
}

// RetryStats contains overall retry statistics
type RetryStats struct {
	TotalRetries      int64                     `json:"total_retries"`
	SuccessfulRetries int64                     `json:"successful_retries"`
	FailedRetries     int64                     `json:"failed_retries"`
	DeadLetterCount   int64                     `json:"dead_letter_count"`
	AverageBackoff    time.Duration             `json:"average_backoff"`
	ActiveRetries     int                       `json:"active_retries"`
	QueueSize         int                       `json:"queue_size"`
	DeadLetterSize    int                       `json:"dead_letter_size"`
	JobStats          map[string]*RetryJobStats `json:"job_stats"`
	LastUpdate        time.Time                 `json:"last_update"`
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:            3,
		DefaultStrategy:       "exponential",
		InitialBackoff:        1 * time.Second,
		MaxBackoff:            5 * time.Minute,
		BackoffMultiplier:     2.0,
		Jitter:                true,
		JitterRange:           0.1,
		WorkerCount:           5,
		QueueSize:             1000,
		DeadLetterQueueSize:   100,
		RetryableErrors:       []string{"timeout", "connection", "temporary"},
		NonRetryableErrors:    []string{"authentication", "authorization", "validation"},
		EnableDeadLetterQueue: true,
		RetryTimeout:          10 * time.Minute,
		Strategies:            make(map[string]interface{}),
	}
}

// NewRetryManager creates a new retry manager
func NewRetryManager(config *RetryConfig, logger common.Logger, metrics common.Metrics) *RetryManager {
	if config == nil {
		config = DefaultRetryConfig()
	}

	rm := &RetryManager{
		config:          config,
		retryQueue:      make(chan *RetryTask, config.QueueSize),
		deadLetterQueue: make(chan *RetryTask, config.DeadLetterQueueSize),
		strategies:      make(map[string]BackoffStrategy),
		activeRetries:   make(map[string]*RetryContext),
		retryStats:      make(map[string]*RetryJobStats),
		stopChannel:     make(chan struct{}),
		logger:          logger,
		metrics:         metrics,
	}

	// Initialize default strategies
	rm.initializeStrategies()

	return rm
}

// Start starts the retry manager
func (rm *RetryManager) Start(ctx context.Context) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.started {
		return common.ErrLifecycleError("start", fmt.Errorf("retry manager already started"))
	}

	if rm.logger != nil {
		rm.logger.Info("starting retry manager",
			logger.Int("worker_count", rm.config.WorkerCount),
			logger.Int("queue_size", rm.config.QueueSize),
		)
	}

	// Start retry workers
	for i := 0; i < rm.config.WorkerCount; i++ {
		rm.wg.Add(1)
		go rm.retryWorker(ctx, i)
	}

	// Start dead letter queue processor
	if rm.config.EnableDeadLetterQueue {
		rm.wg.Add(1)
		go rm.deadLetterWorker(ctx)
	}

	// Start cleanup worker
	rm.wg.Add(1)
	go rm.cleanupWorker(ctx)

	rm.started = true

	if rm.logger != nil {
		rm.logger.Info("retry manager started")
	}

	if rm.metrics != nil {
		rm.metrics.Counter("forge.cron.retry_manager_started").Inc()
	}

	return nil
}

// Stop stops the retry manager
func (rm *RetryManager) Stop(ctx context.Context) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("retry manager not started"))
	}

	if rm.logger != nil {
		rm.logger.Info("stopping retry manager")
	}

	// Signal stop
	close(rm.stopChannel)

	// Wait for workers to finish
	rm.wg.Wait()

	// Close channels
	close(rm.retryQueue)
	close(rm.deadLetterQueue)

	rm.started = false

	if rm.logger != nil {
		rm.logger.Info("retry manager stopped")
	}

	if rm.metrics != nil {
		rm.metrics.Counter("forge.cron.retry_manager_stopped").Inc()
	}

	return nil
}

// ScheduleRetry schedules a job for retry
func (rm *RetryManager) ScheduleRetry(job *cron.Job, execution *cron.JobExecution, err error) error {
	if !rm.started {
		return common.ErrLifecycleError("schedule_retry", fmt.Errorf("retry manager not started"))
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check if retry is allowed
	if !rm.shouldRetry(job, execution, err) {
		return nil // No retry needed
	}

	// Get retry context
	retryCtx := rm.getOrCreateRetryContext(job)

	// Get strategy
	strategy := rm.getStrategy(job.Definition.ID)
	if strategy == nil {
		strategy = rm.strategies[rm.config.DefaultStrategy]
	}

	// Check if we should retry
	if !strategy.ShouldRetry(retryCtx.Attempts, retryCtx.MaxAttempts, err) {
		if rm.config.EnableDeadLetterQueue {
			rm.sendToDeadLetterQueue(job, execution, err, retryCtx.Attempts)
		}
		return nil
	}

	// Calculate delay
	delay := strategy.GetDelay(retryCtx.Attempts, err)
	scheduledAt := time.Now().Add(delay)

	// Create retry task
	task := &RetryTask{
		Job:         job,
		Execution:   execution,
		Error:       err,
		Attempt:     retryCtx.Attempts,
		MaxAttempts: retryCtx.MaxAttempts,
		Strategy:    strategy.Name(),
		ScheduledAt: scheduledAt,
		Context:     make(map[string]interface{}),
		CreatedAt:   time.Now(),
	}

	// Update retry context
	retryCtx.Attempts++
	retryCtx.LastAttempt = time.Now()
	retryCtx.NextAttempt = scheduledAt
	retryCtx.Errors = append(retryCtx.Errors, err)
	retryCtx.UpdatedAt = time.Now()

	// Update statistics
	rm.updateRetryStats(job.Definition.ID, strategy.Name(), delay)

	// Schedule the retry
	go func() {
		time.Sleep(delay)
		select {
		case rm.retryQueue <- task:
			if rm.logger != nil {
				rm.logger.Info("retry task scheduled",
					logger.String("job_id", job.Definition.ID),
					logger.Int("attempt", task.Attempt),
					logger.Duration("delay", delay),
				)
			}
		case <-rm.stopChannel:
			return
		}
	}()

	if rm.metrics != nil {
		rm.metrics.Counter("forge.cron.retries_scheduled").Inc()
		rm.metrics.Histogram("forge.cron.retry_delay").Observe(delay.Seconds())
	}

	return nil
}

// retryWorker processes retry tasks
func (rm *RetryManager) retryWorker(ctx context.Context, workerID int) {
	defer rm.wg.Done()

	if rm.logger != nil {
		rm.logger.Info("retry worker started", logger.Int("worker_id", workerID))
	}

	for {
		select {
		case <-rm.stopChannel:
			return
		case <-ctx.Done():
			return
		case task := <-rm.retryQueue:
			if task != nil {
				rm.processRetryTask(ctx, task, workerID)
			}
		}
	}
}

// processRetryTask processes a single retry task
func (rm *RetryManager) processRetryTask(ctx context.Context, task *RetryTask, workerID int) {
	if rm.logger != nil {
		rm.logger.Info("processing retry task",
			logger.String("job_id", task.Job.Definition.ID),
			logger.Int("attempt", task.Attempt),
			logger.Int("worker_id", workerID),
		)
	}

	// Update execution for retry
	task.Execution.Status = cron.ExecutionStatusRetrying
	task.Execution.Attempt = task.Attempt

	// Call handler retry callback
	if task.Job.Definition.Handler != nil {
		if err := task.Job.Definition.Handler.OnRetry(ctx, task.Job, task.Execution, task.Attempt); err != nil {
			if rm.logger != nil {
				rm.logger.Error("retry callback failed",
					logger.String("job_id", task.Job.Definition.ID),
					logger.Error(err),
				)
			}
		}
	}

	// Execute the job (this would typically be done by the executor)
	// For now, we'll simulate success/failure
	success := rm.simulateRetryExecution(task)

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if success {
		// Retry succeeded
		if rm.logger != nil {
			rm.logger.Info("retry succeeded",
				logger.String("job_id", task.Job.Definition.ID),
				logger.Int("attempt", task.Attempt),
			)
		}

		// Update statistics
		if stats, exists := rm.retryStats[task.Job.Definition.ID]; exists {
			stats.SuccessfulRetries++
		}

		// Remove from active retries
		delete(rm.activeRetries, task.Job.Definition.ID)

		if rm.metrics != nil {
			rm.metrics.Counter("forge.cron.retries_succeeded").Inc()
		}
	} else {
		// Retry failed
		if rm.logger != nil {
			rm.logger.Error("retry failed",
				logger.String("job_id", task.Job.Definition.ID),
				logger.Int("attempt", task.Attempt),
			)
		}

		// Update statistics
		if stats, exists := rm.retryStats[task.Job.Definition.ID]; exists {
			stats.FailedRetries++
		}

		// Check if we should retry again
		if retryCtx, exists := rm.activeRetries[task.Job.Definition.ID]; exists {
			strategy := rm.getStrategy(task.Job.Definition.ID)
			if strategy == nil {
				strategy = rm.strategies[rm.config.DefaultStrategy]
			}

			if !strategy.ShouldRetry(retryCtx.Attempts, retryCtx.MaxAttempts, task.Error) {
				// Max retries reached, send to dead letter queue
				if rm.config.EnableDeadLetterQueue {
					rm.sendToDeadLetterQueue(task.Job, task.Execution, task.Error, retryCtx.Attempts)
				}
				delete(rm.activeRetries, task.Job.Definition.ID)
			}
		}

		if rm.metrics != nil {
			rm.metrics.Counter("forge.cron.retries_failed").Inc()
		}
	}
}

// simulateRetryExecution simulates retry execution (placeholder)
func (rm *RetryManager) simulateRetryExecution(task *RetryTask) bool {
	// This is a placeholder - in reality, this would integrate with the executor
	// For now, simulate 70% success rate
	return time.Now().UnixNano()%10 < 7
}

// deadLetterWorker processes dead letter queue
func (rm *RetryManager) deadLetterWorker(ctx context.Context) {
	defer rm.wg.Done()

	if rm.logger != nil {
		rm.logger.Info("dead letter worker started")
	}

	for {
		select {
		case <-rm.stopChannel:
			return
		case <-ctx.Done():
			return
		case task := <-rm.deadLetterQueue:
			if task != nil {
				rm.processDeadLetterTask(ctx, task)
			}
		}
	}
}

// processDeadLetterTask processes a dead letter task
func (rm *RetryManager) processDeadLetterTask(ctx context.Context, task *RetryTask) {
	if rm.logger != nil {
		rm.logger.Error("job moved to dead letter queue",
			logger.String("job_id", task.Job.Definition.ID),
			logger.Int("final_attempt", task.Attempt),
			logger.Error(task.Error),
		)
	}

	// Here you could implement dead letter queue processing logic
	// such as sending alerts, storing in database, etc.

	if rm.metrics != nil {
		rm.metrics.Counter("forge.cron.dead_letter_jobs").Inc()
	}
}

// cleanupWorker periodically cleans up old retry contexts
func (rm *RetryManager) cleanupWorker(ctx context.Context) {
	defer rm.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopChannel:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.cleanup()
		}
	}
}

// cleanup removes old retry contexts and statistics
func (rm *RetryManager) cleanup() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	cleanupThreshold := now.Add(-24 * time.Hour)

	// Clean up old retry contexts
	for jobID, retryCtx := range rm.activeRetries {
		if retryCtx.UpdatedAt.Before(cleanupThreshold) {
			delete(rm.activeRetries, jobID)
		}
	}

	// Clean up old statistics
	for jobID, stats := range rm.retryStats {
		if stats.LastRetry.Before(cleanupThreshold) {
			delete(rm.retryStats, jobID)
		}
	}

	if rm.logger != nil {
		rm.logger.Debug("retry cleanup completed",
			logger.Int("active_retries", len(rm.activeRetries)),
			logger.Int("job_stats", len(rm.retryStats)),
		)
	}
}

// shouldRetry determines if a job should be retried
func (rm *RetryManager) shouldRetry(job *cron.Job, execution *cron.JobExecution, err error) bool {
	// Check if retries are disabled for this job
	if job.Definition.MaxRetries == 0 {
		return false
	}

	// Check if error is retryable
	if !rm.isRetryableError(err) {
		return false
	}

	// Check if we've exceeded max retries
	if int(job.FailureCount) >= job.Definition.MaxRetries {
		return false
	}

	return true
}

// isRetryableError checks if an error is retryable
func (rm *RetryManager) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errorStr := err.Error()

	// Check non-retryable errors first
	for _, nonRetryable := range rm.config.NonRetryableErrors {
		if contains(errorStr, nonRetryable) {
			return false
		}
	}

	// Check retryable errors
	for _, retryable := range rm.config.RetryableErrors {
		if contains(errorStr, retryable) {
			return true
		}
	}

	// Default to retryable if not explicitly non-retryable
	return true
}

// getOrCreateRetryContext gets or creates retry context for a job
func (rm *RetryManager) getOrCreateRetryContext(job *cron.Job) *RetryContext {
	if retryCtx, exists := rm.activeRetries[job.Definition.ID]; exists {
		return retryCtx
	}

	retryCtx := &RetryContext{
		JobID:       job.Definition.ID,
		Attempts:    0,
		MaxAttempts: job.Definition.MaxRetries,
		LastAttempt: time.Now(),
		Errors:      make([]error, 0),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	rm.activeRetries[job.Definition.ID] = retryCtx
	return retryCtx
}

// getStrategy gets the retry strategy for a job
func (rm *RetryManager) getStrategy(jobID string) BackoffStrategy {
	// This could be configured per job
	return rm.strategies[rm.config.DefaultStrategy]
}

// updateRetryStats updates retry statistics
func (rm *RetryManager) updateRetryStats(jobID, strategyName string, delay time.Duration) {
	if stats, exists := rm.retryStats[jobID]; exists {
		stats.TotalRetries++
		stats.LastRetry = time.Now()
		stats.Strategy = strategyName

		// Update backoff statistics
		if delay > stats.MaxBackoff {
			stats.MaxBackoff = delay
		}
		if stats.MinBackoff == 0 || delay < stats.MinBackoff {
			stats.MinBackoff = delay
		}
		stats.AverageBackoff = (stats.AverageBackoff*time.Duration(stats.TotalRetries-1) + delay) / time.Duration(stats.TotalRetries)
	} else {
		rm.retryStats[jobID] = &RetryJobStats{
			JobID:          jobID,
			TotalRetries:   1,
			AverageBackoff: delay,
			MaxBackoff:     delay,
			MinBackoff:     delay,
			LastRetry:      time.Now(),
			Strategy:       strategyName,
		}
	}
}

// sendToDeadLetterQueue sends a task to the dead letter queue
func (rm *RetryManager) sendToDeadLetterQueue(job *cron.Job, execution *cron.JobExecution, err error, attempts int) {
	task := &RetryTask{
		Job:         job,
		Execution:   execution,
		Error:       err,
		Attempt:     attempts,
		MaxAttempts: job.Definition.MaxRetries,
		CreatedAt:   time.Now(),
	}

	select {
	case rm.deadLetterQueue <- task:
		// Successfully queued
	default:
		// Dead letter queue is full
		if rm.logger != nil {
			rm.logger.Error("dead letter queue full",
				logger.String("job_id", job.Definition.ID),
			)
		}
	}
}

// HealthCheck performs a health check
func (rm *RetryManager) HealthCheck(ctx context.Context) error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if !rm.started {
		return common.ErrHealthCheckFailed("retry-manager", fmt.Errorf("retry manager not started"))
	}

	// Check queue sizes
	queueSize := len(rm.retryQueue)
	deadLetterSize := len(rm.deadLetterQueue)

	if queueSize > int(float64(rm.config.QueueSize)*0.9) {
		return common.ErrHealthCheckFailed("retry-manager", fmt.Errorf("retry queue nearly full: %d/%d", queueSize, rm.config.QueueSize))
	}

	if deadLetterSize > int(float64(rm.config.DeadLetterQueueSize)*0.9) {
		return common.ErrHealthCheckFailed("retry-manager", fmt.Errorf("dead letter queue nearly full: %d/%d", deadLetterSize, rm.config.DeadLetterQueueSize))
	}

	return nil
}

// GetStats returns retry statistics
func (rm *RetryManager) GetStats() *RetryStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := &RetryStats{
		ActiveRetries:  len(rm.activeRetries),
		QueueSize:      len(rm.retryQueue),
		DeadLetterSize: len(rm.deadLetterQueue),
		JobStats:       make(map[string]*RetryJobStats),
		LastUpdate:     time.Now(),
	}

	// Copy job stats
	for jobID, jobStats := range rm.retryStats {
		stats.JobStats[jobID] = &RetryJobStats{
			JobID:             jobStats.JobID,
			TotalRetries:      jobStats.TotalRetries,
			SuccessfulRetries: jobStats.SuccessfulRetries,
			FailedRetries:     jobStats.FailedRetries,
			AverageBackoff:    jobStats.AverageBackoff,
			MaxBackoff:        jobStats.MaxBackoff,
			MinBackoff:        jobStats.MinBackoff,
			LastRetry:         jobStats.LastRetry,
			Strategy:          jobStats.Strategy,
		}

		// Aggregate statistics
		stats.TotalRetries += jobStats.TotalRetries
		stats.SuccessfulRetries += jobStats.SuccessfulRetries
		stats.FailedRetries += jobStats.FailedRetries
	}

	return stats
}

// initializeStrategies initializes default backoff strategies
func (rm *RetryManager) initializeStrategies() {
	// Exponential backoff
	rm.strategies["exponential"] = &ExponentialBackoff{
		InitialDelay: rm.config.InitialBackoff,
		MaxDelay:     rm.config.MaxBackoff,
		Multiplier:   rm.config.BackoffMultiplier,
		Jitter:       rm.config.Jitter,
		JitterRange:  rm.config.JitterRange,
	}

	// Linear backoff
	rm.strategies["linear"] = &LinearBackoff{
		InitialDelay: rm.config.InitialBackoff,
		MaxDelay:     rm.config.MaxBackoff,
		Increment:    rm.config.InitialBackoff,
		Jitter:       rm.config.Jitter,
		JitterRange:  rm.config.JitterRange,
	}

	// Fixed backoff
	rm.strategies["fixed"] = &FixedBackoff{
		Delay:       rm.config.InitialBackoff,
		Jitter:      rm.config.Jitter,
		JitterRange: rm.config.JitterRange,
	}

	// Fibonacci backoff
	rm.strategies["fibonacci"] = &FibonacciBackoff{
		InitialDelay: rm.config.InitialBackoff,
		MaxDelay:     rm.config.MaxBackoff,
		Jitter:       rm.config.Jitter,
		JitterRange:  rm.config.JitterRange,
	}
}

// validateRetryConfig validates retry configuration
func validateRetryConfig(config *RetryConfig) error {
	if config.MaxRetries < 0 {
		return common.ErrValidationError("max_retries", fmt.Errorf("max retries cannot be negative"))
	}

	if config.InitialBackoff <= 0 {
		return common.ErrValidationError("initial_backoff", fmt.Errorf("initial backoff must be positive"))
	}

	if config.MaxBackoff <= 0 {
		return common.ErrValidationError("max_backoff", fmt.Errorf("max backoff must be positive"))
	}

	if config.BackoffMultiplier <= 1.0 {
		return common.ErrValidationError("backoff_multiplier", fmt.Errorf("backoff multiplier must be greater than 1"))
	}

	if config.WorkerCount <= 0 {
		return common.ErrValidationError("worker_count", fmt.Errorf("worker count must be positive"))
	}

	if config.QueueSize <= 0 {
		return common.ErrValidationError("queue_size", fmt.Errorf("queue size must be positive"))
	}

	if config.JitterRange < 0 || config.JitterRange > 1 {
		return common.ErrValidationError("jitter_range", fmt.Errorf("jitter range must be between 0 and 1"))
	}

	return nil
}

// Implementation of BackoffStrategy interface for each strategy

// ExponentialBackoff implementation
func (eb *ExponentialBackoff) GetDelay(attempt int, lastError error) time.Duration {
	if attempt <= 0 {
		return 0
	}

	delay := float64(eb.InitialDelay)
	for i := 1; i < attempt; i++ {
		delay *= eb.Multiplier
	}

	if time.Duration(delay) > eb.MaxDelay {
		delay = float64(eb.MaxDelay)
	}

	if eb.Jitter {
		delay = eb.addJitter(delay)
	}

	return time.Duration(delay)
}

func (eb *ExponentialBackoff) ShouldRetry(attempt int, maxAttempts int, err error) bool {
	return attempt < maxAttempts
}

func (eb *ExponentialBackoff) Name() string {
	return "exponential"
}

func (eb *ExponentialBackoff) Configure(config map[string]interface{}) error {
	// Implementation for configuring exponential backoff
	return nil
}

func (eb *ExponentialBackoff) addJitter(delay float64) float64 {
	if !eb.Jitter {
		return delay
	}

	jitter := delay * eb.JitterRange * (2*rand.Float64() - 1)
	return delay + jitter
}

// LinearBackoff implementation
func (lb *LinearBackoff) GetDelay(attempt int, lastError error) time.Duration {
	if attempt <= 0 {
		return 0
	}

	delay := float64(lb.InitialDelay) + float64(lb.Increment)*float64(attempt-1)

	if time.Duration(delay) > lb.MaxDelay {
		delay = float64(lb.MaxDelay)
	}

	if lb.Jitter {
		delay = lb.addJitter(delay)
	}

	return time.Duration(delay)
}

func (lb *LinearBackoff) ShouldRetry(attempt int, maxAttempts int, err error) bool {
	return attempt < maxAttempts
}

func (lb *LinearBackoff) Name() string {
	return "linear"
}

func (lb *LinearBackoff) Configure(config map[string]interface{}) error {
	return nil
}

func (lb *LinearBackoff) addJitter(delay float64) float64 {
	if !lb.Jitter {
		return delay
	}

	jitter := delay * lb.JitterRange * (2*rand.Float64() - 1)
	return delay + jitter
}

// FixedBackoff implementation
func (fb *FixedBackoff) GetDelay(attempt int, lastError error) time.Duration {
	delay := float64(fb.Delay)

	if fb.Jitter {
		delay = fb.addJitter(delay)
	}

	return time.Duration(delay)
}

func (fb *FixedBackoff) ShouldRetry(attempt int, maxAttempts int, err error) bool {
	return attempt < maxAttempts
}

func (fb *FixedBackoff) Name() string {
	return "fixed"
}

func (fb *FixedBackoff) Configure(config map[string]interface{}) error {
	return nil
}

func (fb *FixedBackoff) addJitter(delay float64) float64 {
	if !fb.Jitter {
		return delay
	}

	jitter := delay * fb.JitterRange * (2*rand.Float64() - 1)
	return delay + jitter
}

// FibonacciBackoff implementation
func (fib *FibonacciBackoff) GetDelay(attempt int, lastError error) time.Duration {
	if attempt <= 0 {
		return 0
	}

	fibValue := fib.fibonacci(attempt)
	delay := float64(fib.InitialDelay) * float64(fibValue)

	if time.Duration(delay) > fib.MaxDelay {
		delay = float64(fib.MaxDelay)
	}

	if fib.Jitter {
		delay = fib.addJitter(delay)
	}

	return time.Duration(delay)
}

func (fib *FibonacciBackoff) ShouldRetry(attempt int, maxAttempts int, err error) bool {
	return attempt < maxAttempts
}

func (fib *FibonacciBackoff) Name() string {
	return "fibonacci"
}

func (fib *FibonacciBackoff) Configure(config map[string]interface{}) error {
	return nil
}

func (fib *FibonacciBackoff) fibonacci(n int) int {
	if n <= 1 {
		return n
	}

	a, b := 0, 1
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

func (fib *FibonacciBackoff) addJitter(delay float64) float64 {
	if !fib.Jitter {
		return delay
	}

	jitter := delay * fib.JitterRange * (2*rand.Float64() - 1)
	return delay + jitter
}

// CustomBackoff implementation
func (cb *CustomBackoff) GetDelay(attempt int, lastError error) time.Duration {
	if cb.DelayFunc != nil {
		return cb.DelayFunc(attempt, lastError)
	}
	return 0
}

func (cb *CustomBackoff) ShouldRetry(attempt int, maxAttempts int, err error) bool {
	if cb.ShouldRetryFunc != nil {
		return cb.ShouldRetryFunc(attempt, maxAttempts, err)
	}
	return attempt < maxAttempts
}

func (cb *CustomBackoff) Name() string {
	return "custom"
}

func (cb *CustomBackoff) Configure(config map[string]interface{}) error {
	return nil
}

// Utility functions

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsMiddle(s, substr))))
}

// containsMiddle checks if substring is in the middle of string
func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
