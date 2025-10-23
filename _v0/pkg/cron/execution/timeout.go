package execution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	cron "github.com/xraph/forge/v0/pkg/cron/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// TimeoutManager manages job execution timeouts
type TimeoutManager struct {
	config       *TimeoutConfig
	activeTimers map[string]*TimeoutContext
	timeoutQueue chan *TimeoutEvent
	policies     map[string]TimeoutPolicy

	// Timeout statistics
	timeoutStats map[string]*TimeoutJobStats
	mu           sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// TimeoutConfig contains timeout configuration
type TimeoutConfig struct {
	DefaultTimeout     time.Duration          `json:"default_timeout" yaml:"default_timeout"`
	MaxTimeout         time.Duration          `json:"max_timeout" yaml:"max_timeout"`
	MinTimeout         time.Duration          `json:"min_timeout" yaml:"min_timeout"`
	CheckInterval      time.Duration          `json:"check_interval" yaml:"check_interval"`
	GracePeriod        time.Duration          `json:"grace_period" yaml:"grace_period"`
	WorkerCount        int                    `json:"worker_count" yaml:"worker_count"`
	QueueSize          int                    `json:"queue_size" yaml:"queue_size"`
	EnableGracefulStop bool                   `json:"enable_graceful_stop" yaml:"enable_graceful_stop"`
	TimeoutPolicies    map[string]interface{} `json:"timeout_policies" yaml:"timeout_policies"`
	AlertThreshold     time.Duration          `json:"alert_threshold" yaml:"alert_threshold"`
	EnableMetrics      bool                   `json:"enable_metrics" yaml:"enable_metrics"`
}

// TimeoutContext tracks timeout information for a job execution
type TimeoutContext struct {
	ExecutionID     string
	JobID           string
	StartTime       time.Time
	Timeout         time.Duration
	GracePeriod     time.Duration
	Policy          TimeoutPolicy
	Timer           *time.Timer
	GraceTimer      *time.Timer
	Context         context.Context
	CancelFunc      context.CancelFunc
	GraceCancelFunc context.CancelFunc
	Status          TimeoutStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TimeoutEvent represents a timeout event
type TimeoutEvent struct {
	Type        TimeoutEventType
	ExecutionID string
	JobID       string
	Timeout     time.Duration
	Elapsed     time.Duration
	Timestamp   time.Time
	Context     map[string]interface{}
}

// TimeoutEventType represents the type of timeout event
type TimeoutEventType string

const (
	TimeoutEventWarning   TimeoutEventType = "warning"
	TimeoutEventTimeout   TimeoutEventType = "timeout"
	TimeoutEventGrace     TimeoutEventType = "grace"
	TimeoutEventKilled    TimeoutEventType = "killed"
	TimeoutEventCompleted TimeoutEventType = "completed"
	TimeoutEventCancelled TimeoutEventType = "cancelled"
)

// TimeoutStatus represents the status of a timeout
type TimeoutStatus string

const (
	TimeoutStatusActive    TimeoutStatus = "active"
	TimeoutStatusWarning   TimeoutStatus = "warning"
	TimeoutStatusTimeout   TimeoutStatus = "timeout"
	TimeoutStatusGrace     TimeoutStatus = "grace"
	TimeoutStatusKilled    TimeoutStatus = "killed"
	TimeoutStatusCompleted TimeoutStatus = "completed"
	TimeoutStatusCancelled TimeoutStatus = "cancelled"
)

// TimeoutPolicy defines how timeouts should be handled
type TimeoutPolicy interface {
	GetTimeout(job *cron.Job) time.Duration
	GetGracePeriod(job *cron.Job) time.Duration
	ShouldAlert(elapsed time.Duration, timeout time.Duration) bool
	OnTimeout(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error
	OnGrace(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error
	OnKill(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error
	Name() string
}

// DefaultTimeoutPolicy implements the default timeout policy
type DefaultTimeoutPolicy struct {
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	MinTimeout     time.Duration
	GracePeriod    time.Duration
	AlertThreshold time.Duration
	EnableGraceful bool
}

// LinearTimeoutPolicy implements linear timeout based on job complexity
type LinearTimeoutPolicy struct {
	BaseTimeout    time.Duration
	TimeoutPerUnit time.Duration
	MaxTimeout     time.Duration
	GracePeriod    time.Duration
	AlertThreshold time.Duration
}

// CustomTimeoutPolicy allows custom timeout logic
type CustomTimeoutPolicy struct {
	TimeoutFunc   func(job *cron.Job) time.Duration
	GraceFunc     func(job *cron.Job) time.Duration
	AlertFunc     func(elapsed, timeout time.Duration) bool
	OnTimeoutFunc func(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error
	OnGraceFunc   func(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error
	OnKillFunc    func(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error
	PolicyName    string
}

// TimeoutJobStats tracks timeout statistics for a job
type TimeoutJobStats struct {
	JobID            string        `json:"job_id"`
	TotalExecutions  int64         `json:"total_executions"`
	TimeoutCount     int64         `json:"timeout_count"`
	WarningCount     int64         `json:"warning_count"`
	KilledCount      int64         `json:"killed_count"`
	AverageExecution time.Duration `json:"average_execution"`
	MaxExecution     time.Duration `json:"max_execution"`
	MinExecution     time.Duration `json:"min_execution"`
	TimeoutRate      float64       `json:"timeout_rate"`
	LastTimeout      time.Time     `json:"last_timeout"`
	Policy           string        `json:"policy"`
}

// TimeoutManagerStats contains overall timeout statistics
type TimeoutManagerStats struct {
	ActiveTimeouts int                            `json:"active_timeouts"`
	TotalTimeouts  int64                          `json:"total_timeouts"`
	TimeoutRate    float64                        `json:"timeout_rate"`
	AverageTimeout time.Duration                  `json:"average_timeout"`
	JobStats       map[string]*TimeoutJobStats    `json:"job_stats"`
	PolicyStats    map[string]*TimeoutPolicyStats `json:"policy_stats"`
	LastUpdate     time.Time                      `json:"last_update"`
}

// TimeoutPolicyStats tracks statistics for a timeout policy
type TimeoutPolicyStats struct {
	PolicyName     string        `json:"policy_name"`
	UsageCount     int64         `json:"usage_count"`
	TimeoutCount   int64         `json:"timeout_count"`
	AverageTimeout time.Duration `json:"average_timeout"`
	LastUsed       time.Time     `json:"last_used"`
}

// DefaultTimeoutConfig returns default timeout configuration
func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		DefaultTimeout:     30 * time.Minute,
		MaxTimeout:         2 * time.Hour,
		MinTimeout:         30 * time.Second,
		CheckInterval:      30 * time.Second,
		GracePeriod:        30 * time.Second,
		WorkerCount:        3,
		QueueSize:          1000,
		EnableGracefulStop: true,
		TimeoutPolicies:    make(map[string]interface{}),
		AlertThreshold:     20 * time.Minute,
		EnableMetrics:      true,
	}
}

// NewTimeoutManager creates a new timeout manager
func NewTimeoutManager(defaultTimeout time.Duration, logger common.Logger, metrics common.Metrics) *TimeoutManager {
	config := DefaultTimeoutConfig()
	if defaultTimeout > 0 {
		config.DefaultTimeout = defaultTimeout
	}

	tm := &TimeoutManager{
		config:       config,
		activeTimers: make(map[string]*TimeoutContext),
		timeoutQueue: make(chan *TimeoutEvent, config.QueueSize),
		policies:     make(map[string]TimeoutPolicy),
		timeoutStats: make(map[string]*TimeoutJobStats),
		stopChannel:  make(chan struct{}),
		logger:       logger,
		metrics:      metrics,
	}

	// Initialize default policies
	tm.initializePolicies()

	return tm
}

// Start starts the timeout manager
func (tm *TimeoutManager) Start(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.started {
		return common.ErrLifecycleError("start", fmt.Errorf("timeout manager already started"))
	}

	if tm.logger != nil {
		tm.logger.Info("starting timeout manager",
			logger.Duration("default_timeout", tm.config.DefaultTimeout),
			logger.Int("worker_count", tm.config.WorkerCount),
		)
	}

	// Start timeout workers
	for i := 0; i < tm.config.WorkerCount; i++ {
		tm.wg.Add(1)
		go tm.timeoutWorker(ctx, i)
	}

	// Start timeout checker
	tm.wg.Add(1)
	go tm.timeoutChecker(ctx)

	// Start cleanup worker
	tm.wg.Add(1)
	go tm.cleanupWorker(ctx)

	tm.started = true

	if tm.logger != nil {
		tm.logger.Info("timeout manager started")
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.cron.timeout_manager_started").Inc()
	}

	return nil
}

// Stop stops the timeout manager
func (tm *TimeoutManager) Stop(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("timeout manager not started"))
	}

	if tm.logger != nil {
		tm.logger.Info("stopping timeout manager")
	}

	// Cancel all active timers
	for _, timeoutCtx := range tm.activeTimers {
		if timeoutCtx.Timer != nil {
			timeoutCtx.Timer.Stop()
		}
		if timeoutCtx.GraceTimer != nil {
			timeoutCtx.GraceTimer.Stop()
		}
		if timeoutCtx.CancelFunc != nil {
			timeoutCtx.CancelFunc()
		}
		if timeoutCtx.GraceCancelFunc != nil {
			timeoutCtx.GraceCancelFunc()
		}
	}

	// Signal stop
	close(tm.stopChannel)

	// Wait for workers to finish
	tm.wg.Wait()

	// Close timeout queue
	close(tm.timeoutQueue)

	tm.started = false

	if tm.logger != nil {
		tm.logger.Info("timeout manager stopped")
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.cron.timeout_manager_stopped").Inc()
	}

	return nil
}

// StartTimeout starts a timeout for a job execution
func (tm *TimeoutManager) StartTimeout(ctx context.Context, job *cron.Job, execution *cron.JobExecution) (context.Context, context.CancelFunc, error) {
	if !tm.started {
		return nil, nil, common.ErrLifecycleError("start_timeout", fmt.Errorf("timeout manager not started"))
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check if timeout already exists
	if _, exists := tm.activeTimers[execution.ID]; exists {
		return nil, nil, common.ErrValidationError("execution_id", fmt.Errorf("timeout already exists for execution %s", execution.ID))
	}

	// Get timeout policy
	policy := tm.getPolicy(job)
	timeout := policy.GetTimeout(job)
	gracePeriod := policy.GetGracePeriod(job)

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	_, graceCancel := context.WithCancel(context.Background())

	// Create timeout context entry
	timeoutContext := &TimeoutContext{
		ExecutionID:     execution.ID,
		JobID:           job.Definition.ID,
		StartTime:       time.Now(),
		Timeout:         timeout,
		GracePeriod:     gracePeriod,
		Policy:          policy,
		Context:         timeoutCtx,
		CancelFunc:      cancel,
		GraceCancelFunc: graceCancel,
		Status:          TimeoutStatusActive,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Create timeout timer
	timeoutContext.Timer = time.NewTimer(timeout)
	go tm.handleTimeout(timeoutContext)

	// Store timeout context
	tm.activeTimers[execution.ID] = timeoutContext

	// Update statistics
	tm.updateTimeoutStats(job.Definition.ID, policy.Name(), 0, false)

	if tm.logger != nil {
		tm.logger.Info("timeout started",
			logger.String("job_id", job.Definition.ID),
			logger.String("execution_id", execution.ID),
			logger.Duration("timeout", timeout),
			logger.String("policy", policy.Name()),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.cron.timeouts_started").Inc()
		tm.metrics.Histogram("forge.cron.timeout_duration").Observe(timeout.Seconds())
	}

	return timeoutCtx, cancel, nil
}

// CancelTimeout cancels a timeout for a job execution
func (tm *TimeoutManager) CancelTimeout(executionID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	timeoutContext, exists := tm.activeTimers[executionID]
	if !exists {
		return common.ErrServiceNotFound(executionID)
	}

	// Stop timers
	if timeoutContext.Timer != nil {
		timeoutContext.Timer.Stop()
	}
	if timeoutContext.GraceTimer != nil {
		timeoutContext.GraceTimer.Stop()
	}

	// Cancel contexts
	if timeoutContext.CancelFunc != nil {
		timeoutContext.CancelFunc()
	}
	if timeoutContext.GraceCancelFunc != nil {
		timeoutContext.GraceCancelFunc()
	}

	// Update status
	timeoutContext.Status = TimeoutStatusCancelled
	timeoutContext.UpdatedAt = time.Now()

	// Send timeout event
	event := &TimeoutEvent{
		Type:        TimeoutEventCancelled,
		ExecutionID: executionID,
		JobID:       timeoutContext.JobID,
		Timeout:     timeoutContext.Timeout,
		Elapsed:     time.Since(timeoutContext.StartTime),
		Timestamp:   time.Now(),
	}

	select {
	case tm.timeoutQueue <- event:
	default:
		// Queue full, log warning
		if tm.logger != nil {
			tm.logger.Warn("timeout queue full", logger.String("execution_id", executionID))
		}
	}

	// Remove from active timers
	delete(tm.activeTimers, executionID)

	if tm.logger != nil {
		tm.logger.Info("timeout cancelled",
			logger.String("execution_id", executionID),
			logger.Duration("elapsed", time.Since(timeoutContext.StartTime)),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.cron.timeouts_cancelled").Inc()
	}

	return nil
}

// CompleteTimeout marks a timeout as completed (job finished before timeout)
func (tm *TimeoutManager) CompleteTimeout(executionID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	timeoutContext, exists := tm.activeTimers[executionID]
	if !exists {
		return common.ErrServiceNotFound(executionID)
	}

	elapsed := time.Since(timeoutContext.StartTime)

	// Stop timers
	if timeoutContext.Timer != nil {
		timeoutContext.Timer.Stop()
	}
	if timeoutContext.GraceTimer != nil {
		timeoutContext.GraceTimer.Stop()
	}

	// Update status
	timeoutContext.Status = TimeoutStatusCompleted
	timeoutContext.UpdatedAt = time.Now()

	// Send timeout event
	event := &TimeoutEvent{
		Type:        TimeoutEventCompleted,
		ExecutionID: executionID,
		JobID:       timeoutContext.JobID,
		Timeout:     timeoutContext.Timeout,
		Elapsed:     elapsed,
		Timestamp:   time.Now(),
	}

	select {
	case tm.timeoutQueue <- event:
	default:
		// Queue full, log warning
		if tm.logger != nil {
			tm.logger.Warn("timeout queue full", logger.String("execution_id", executionID))
		}
	}

	// Update statistics
	tm.updateTimeoutStats(timeoutContext.JobID, timeoutContext.Policy.Name(), elapsed, false)

	// Remove from active timers
	delete(tm.activeTimers, executionID)

	if tm.logger != nil {
		tm.logger.Info("timeout completed",
			logger.String("execution_id", executionID),
			logger.Duration("elapsed", elapsed),
			logger.Duration("timeout", timeoutContext.Timeout),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.cron.timeouts_completed").Inc()
		tm.metrics.Histogram("forge.cron.execution_duration").Observe(elapsed.Seconds())
	}

	return nil
}

// handleTimeout handles timeout expiration
func (tm *TimeoutManager) handleTimeout(timeoutContext *TimeoutContext) {
	select {
	case <-timeoutContext.Timer.C:
		// Timeout occurred
		tm.processTimeout(timeoutContext)
	case <-tm.stopChannel:
		return
	}
}

// processTimeout processes a timeout event
func (tm *TimeoutManager) processTimeout(timeoutContext *TimeoutContext) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	elapsed := time.Since(timeoutContext.StartTime)

	// Update status
	timeoutContext.Status = TimeoutStatusTimeout
	timeoutContext.UpdatedAt = time.Now()

	// Send timeout event
	event := &TimeoutEvent{
		Type:        TimeoutEventTimeout,
		ExecutionID: timeoutContext.ExecutionID,
		JobID:       timeoutContext.JobID,
		Timeout:     timeoutContext.Timeout,
		Elapsed:     elapsed,
		Timestamp:   time.Now(),
	}

	select {
	case tm.timeoutQueue <- event:
	default:
		// Queue full, log warning
		if tm.logger != nil {
			tm.logger.Warn("timeout queue full", logger.String("execution_id", timeoutContext.ExecutionID))
		}
	}

	// Cancel the execution context
	if timeoutContext.CancelFunc != nil {
		timeoutContext.CancelFunc()
	}

	// Start grace period if enabled
	if tm.config.EnableGracefulStop && timeoutContext.GracePeriod > 0 {
		timeoutContext.Status = TimeoutStatusGrace
		timeoutContext.GraceTimer = time.NewTimer(timeoutContext.GracePeriod)

		go func() {
			select {
			case <-timeoutContext.GraceTimer.C:
				tm.processGraceTimeout(timeoutContext)
			case <-tm.stopChannel:
				return
			}
		}()
	}

	// Update statistics
	tm.updateTimeoutStats(timeoutContext.JobID, timeoutContext.Policy.Name(), elapsed, true)

	if tm.logger != nil {
		tm.logger.Warn("job execution timed out",
			logger.String("job_id", timeoutContext.JobID),
			logger.String("execution_id", timeoutContext.ExecutionID),
			logger.Duration("timeout", timeoutContext.Timeout),
			logger.Duration("elapsed", elapsed),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.cron.timeouts_occurred").Inc()
	}
}

// processGraceTimeout processes grace period timeout
func (tm *TimeoutManager) processGraceTimeout(timeoutContext *TimeoutContext) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Update status
	timeoutContext.Status = TimeoutStatusKilled
	timeoutContext.UpdatedAt = time.Now()

	// Send kill event
	event := &TimeoutEvent{
		Type:        TimeoutEventKilled,
		ExecutionID: timeoutContext.ExecutionID,
		JobID:       timeoutContext.JobID,
		Timeout:     timeoutContext.Timeout,
		Elapsed:     time.Since(timeoutContext.StartTime),
		Timestamp:   time.Now(),
	}

	select {
	case tm.timeoutQueue <- event:
	default:
		// Queue full, log warning
		if tm.logger != nil {
			tm.logger.Warn("timeout queue full", logger.String("execution_id", timeoutContext.ExecutionID))
		}
	}

	// Cancel grace context
	if timeoutContext.GraceCancelFunc != nil {
		timeoutContext.GraceCancelFunc()
	}

	// Update statistics
	if stats, exists := tm.timeoutStats[timeoutContext.JobID]; exists {
		stats.KilledCount++
	}

	// Remove from active timers
	delete(tm.activeTimers, timeoutContext.ExecutionID)

	if tm.logger != nil {
		tm.logger.Error("job execution killed after grace period",
			logger.String("job_id", timeoutContext.JobID),
			logger.String("execution_id", timeoutContext.ExecutionID),
			logger.Duration("grace_period", timeoutContext.GracePeriod),
		)
	}

	if tm.metrics != nil {
		tm.metrics.Counter("forge.cron.executions_killed").Inc()
	}
}

// timeoutWorker processes timeout events
func (tm *TimeoutManager) timeoutWorker(ctx context.Context, workerID int) {
	defer tm.wg.Done()

	if tm.logger != nil {
		tm.logger.Info("timeout worker started", logger.Int("worker_id", workerID))
	}

	for {
		select {
		case <-tm.stopChannel:
			return
		case <-ctx.Done():
			return
		case event := <-tm.timeoutQueue:
			if event != nil {
				tm.processTimeoutEvent(ctx, event, workerID)
			}
		}
	}
}

// processTimeoutEvent processes a timeout event
func (tm *TimeoutManager) processTimeoutEvent(ctx context.Context, event *TimeoutEvent, workerID int) {
	if tm.logger != nil {
		tm.logger.Info("processing timeout event",
			logger.String("type", string(event.Type)),
			logger.String("execution_id", event.ExecutionID),
			logger.Int("worker_id", workerID),
		)
	}

	// Here you would implement specific timeout event processing
	// such as sending alerts, updating databases, etc.

	switch event.Type {
	case TimeoutEventWarning:
		tm.handleWarningEvent(ctx, event)
	case TimeoutEventTimeout:
		tm.handleTimeoutEvent(ctx, event)
	case TimeoutEventKilled:
		tm.handleKilledEvent(ctx, event)
	}
}

// handleWarningEvent handles warning events
func (tm *TimeoutManager) handleWarningEvent(ctx context.Context, event *TimeoutEvent) {
	if tm.logger != nil {
		tm.logger.Warn("job execution approaching timeout",
			logger.String("job_id", event.JobID),
			logger.String("execution_id", event.ExecutionID),
			logger.Duration("elapsed", event.Elapsed),
			logger.Duration("timeout", event.Timeout),
		)
	}
}

// handleTimeoutEvent handles timeout events
func (tm *TimeoutManager) handleTimeoutEvent(ctx context.Context, event *TimeoutEvent) {
	if tm.logger != nil {
		tm.logger.Error("job execution timeout",
			logger.String("job_id", event.JobID),
			logger.String("execution_id", event.ExecutionID),
			logger.Duration("timeout", event.Timeout),
		)
	}

	// Send alerts if configured
	// This would integrate with alerting system
}

// handleKilledEvent handles killed events
func (tm *TimeoutManager) handleKilledEvent(ctx context.Context, event *TimeoutEvent) {
	if tm.logger != nil {
		tm.logger.Error("job execution killed",
			logger.String("job_id", event.JobID),
			logger.String("execution_id", event.ExecutionID),
		)
	}

	// Send critical alerts
	// This would integrate with alerting system
}

// timeoutChecker periodically checks for timeouts
func (tm *TimeoutManager) timeoutChecker(ctx context.Context) {
	defer tm.wg.Done()

	ticker := time.NewTicker(tm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.stopChannel:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			tm.checkTimeouts()
		}
	}
}

// checkTimeouts checks for approaching timeouts
func (tm *TimeoutManager) checkTimeouts() {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	now := time.Now()

	for _, timeoutContext := range tm.activeTimers {
		if timeoutContext.Status != TimeoutStatusActive {
			continue
		}

		elapsed := now.Sub(timeoutContext.StartTime)
		// remaining := timeoutContext.Timeout - elapsed

		// Check if we should send a warning
		if timeoutContext.Policy.ShouldAlert(elapsed, timeoutContext.Timeout) {
			event := &TimeoutEvent{
				Type:        TimeoutEventWarning,
				ExecutionID: timeoutContext.ExecutionID,
				JobID:       timeoutContext.JobID,
				Timeout:     timeoutContext.Timeout,
				Elapsed:     elapsed,
				Timestamp:   now,
			}

			select {
			case tm.timeoutQueue <- event:
			default:
				// Queue full, skip warning
			}

			// Update status
			timeoutContext.Status = TimeoutStatusWarning
			timeoutContext.UpdatedAt = now

			// Update statistics
			if stats, exists := tm.timeoutStats[timeoutContext.JobID]; exists {
				stats.WarningCount++
			}
		}
	}
}

// cleanupWorker periodically cleans up old timeout statistics
func (tm *TimeoutManager) cleanupWorker(ctx context.Context) {
	defer tm.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-tm.stopChannel:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			tm.cleanup()
		}
	}
}

// cleanup removes old timeout statistics
func (tm *TimeoutManager) cleanup() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()
	cleanupThreshold := now.Add(-24 * time.Hour)

	// Clean up old statistics
	for jobID, stats := range tm.timeoutStats {
		if stats.LastTimeout.Before(cleanupThreshold) {
			delete(tm.timeoutStats, jobID)
		}
	}

	if tm.logger != nil {
		tm.logger.Debug("timeout cleanup completed",
			logger.Int("active_timers", len(tm.activeTimers)),
			logger.Int("job_stats", len(tm.timeoutStats)),
		)
	}
}

// getPolicy gets the timeout policy for a job
func (tm *TimeoutManager) getPolicy(job *cron.Job) TimeoutPolicy {
	// This could be configured per job
	// For now, return default policy
	return tm.policies["default"]
}

// updateTimeoutStats updates timeout statistics
func (tm *TimeoutManager) updateTimeoutStats(jobID, policyName string, elapsed time.Duration, timedOut bool) {
	if stats, exists := tm.timeoutStats[jobID]; exists {
		stats.TotalExecutions++
		if timedOut {
			stats.TimeoutCount++
			stats.LastTimeout = time.Now()
		}

		// Update execution time statistics
		if elapsed > 0 {
			if elapsed > stats.MaxExecution {
				stats.MaxExecution = elapsed
			}
			if stats.MinExecution == 0 || elapsed < stats.MinExecution {
				stats.MinExecution = elapsed
			}
			stats.AverageExecution = (stats.AverageExecution*time.Duration(stats.TotalExecutions-1) + elapsed) / time.Duration(stats.TotalExecutions)
		}

		// Update timeout rate
		stats.TimeoutRate = float64(stats.TimeoutCount) / float64(stats.TotalExecutions)
		stats.Policy = policyName
	} else {
		stats := &TimeoutJobStats{
			JobID:            jobID,
			TotalExecutions:  1,
			AverageExecution: elapsed,
			MaxExecution:     elapsed,
			MinExecution:     elapsed,
			Policy:           policyName,
		}

		if timedOut {
			stats.TimeoutCount = 1
			stats.TimeoutRate = 1.0
			stats.LastTimeout = time.Now()
		}

		tm.timeoutStats[jobID] = stats
	}
}

// initializePolicies initializes default timeout policies
func (tm *TimeoutManager) initializePolicies() {
	// Default timeout policy
	tm.policies["default"] = &DefaultTimeoutPolicy{
		DefaultTimeout: tm.config.DefaultTimeout,
		MaxTimeout:     tm.config.MaxTimeout,
		MinTimeout:     tm.config.MinTimeout,
		GracePeriod:    tm.config.GracePeriod,
		AlertThreshold: tm.config.AlertThreshold,
		EnableGraceful: tm.config.EnableGracefulStop,
	}

	// Linear timeout policy
	tm.policies["linear"] = &LinearTimeoutPolicy{
		BaseTimeout:    tm.config.DefaultTimeout,
		TimeoutPerUnit: 1 * time.Minute,
		MaxTimeout:     tm.config.MaxTimeout,
		GracePeriod:    tm.config.GracePeriod,
		AlertThreshold: tm.config.AlertThreshold,
	}
}

// HealthCheck performs a health check
func (tm *TimeoutManager) HealthCheck(ctx context.Context) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if !tm.started {
		return common.ErrHealthCheckFailed("timeout-manager", fmt.Errorf("timeout manager not started"))
	}

	// Check queue size
	queueSize := len(tm.timeoutQueue)
	if queueSize > int(float64(tm.config.QueueSize)*0.9) {
		return common.ErrHealthCheckFailed("timeout-manager", fmt.Errorf("timeout queue nearly full: %d/%d", queueSize, tm.config.QueueSize))
	}

	// Check for stuck timers
	now := time.Now()
	stuckThreshold := now.Add(-tm.config.MaxTimeout * 2)

	for _, timeoutContext := range tm.activeTimers {
		if timeoutContext.CreatedAt.Before(stuckThreshold) {
			return common.ErrHealthCheckFailed("timeout-manager", fmt.Errorf("found stuck timeout: %s", timeoutContext.ExecutionID))
		}
	}

	return nil
}

// GetStats returns timeout statistics
func (tm *TimeoutManager) GetStats() *TimeoutManagerStats {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	stats := &TimeoutManagerStats{
		ActiveTimeouts: len(tm.activeTimers),
		JobStats:       make(map[string]*TimeoutJobStats),
		PolicyStats:    make(map[string]*TimeoutPolicyStats),
		LastUpdate:     time.Now(),
	}

	var totalTimeouts int64
	var totalExecutions int64

	// Copy job stats
	for jobID, jobStats := range tm.timeoutStats {
		stats.JobStats[jobID] = &TimeoutJobStats{
			JobID:            jobStats.JobID,
			TotalExecutions:  jobStats.TotalExecutions,
			TimeoutCount:     jobStats.TimeoutCount,
			WarningCount:     jobStats.WarningCount,
			KilledCount:      jobStats.KilledCount,
			AverageExecution: jobStats.AverageExecution,
			MaxExecution:     jobStats.MaxExecution,
			MinExecution:     jobStats.MinExecution,
			TimeoutRate:      jobStats.TimeoutRate,
			LastTimeout:      jobStats.LastTimeout,
			Policy:           jobStats.Policy,
		}

		totalTimeouts += jobStats.TimeoutCount
		totalExecutions += jobStats.TotalExecutions
	}

	// Calculate overall timeout rate
	if totalExecutions > 0 {
		stats.TimeoutRate = float64(totalTimeouts) / float64(totalExecutions)
	}

	stats.TotalTimeouts = totalTimeouts

	return stats
}

// Implementation of TimeoutPolicy interface

// DefaultTimeoutPolicy implementation
func (dtp *DefaultTimeoutPolicy) GetTimeout(job *cron.Job) time.Duration {
	if job.Definition.Timeout > 0 {
		timeout := job.Definition.Timeout
		if timeout > dtp.MaxTimeout {
			timeout = dtp.MaxTimeout
		}
		if timeout < dtp.MinTimeout {
			timeout = dtp.MinTimeout
		}
		return timeout
	}
	return dtp.DefaultTimeout
}

func (dtp *DefaultTimeoutPolicy) GetGracePeriod(job *cron.Job) time.Duration {
	if dtp.EnableGraceful {
		return dtp.GracePeriod
	}
	return 0
}

func (dtp *DefaultTimeoutPolicy) ShouldAlert(elapsed time.Duration, timeout time.Duration) bool {
	return elapsed > dtp.AlertThreshold && elapsed < timeout
}

func (dtp *DefaultTimeoutPolicy) OnTimeout(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	// Default timeout handling
	return nil
}

func (dtp *DefaultTimeoutPolicy) OnGrace(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	// Default grace period handling
	return nil
}

func (dtp *DefaultTimeoutPolicy) OnKill(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	// Default kill handling
	return nil
}

func (dtp *DefaultTimeoutPolicy) Name() string {
	return "default"
}

// LinearTimeoutPolicy implementation
func (ltp *LinearTimeoutPolicy) GetTimeout(job *cron.Job) time.Duration {
	// Calculate timeout based on job complexity or size
	timeout := ltp.BaseTimeout

	// This is a simplified example - in practice, you'd analyze job requirements
	if complexity, exists := job.Definition.Config["complexity"]; exists {
		if complexityInt, ok := complexity.(int); ok {
			timeout += time.Duration(complexityInt) * ltp.TimeoutPerUnit
		}
	}

	if timeout > ltp.MaxTimeout {
		timeout = ltp.MaxTimeout
	}

	return timeout
}

func (ltp *LinearTimeoutPolicy) GetGracePeriod(job *cron.Job) time.Duration {
	return ltp.GracePeriod
}

func (ltp *LinearTimeoutPolicy) ShouldAlert(elapsed time.Duration, timeout time.Duration) bool {
	return elapsed > ltp.AlertThreshold && elapsed < timeout
}

func (ltp *LinearTimeoutPolicy) OnTimeout(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	return nil
}

func (ltp *LinearTimeoutPolicy) OnGrace(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	return nil
}

func (ltp *LinearTimeoutPolicy) OnKill(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	return nil
}

func (ltp *LinearTimeoutPolicy) Name() string {
	return "linear"
}

// CustomTimeoutPolicy implementation
func (ctp *CustomTimeoutPolicy) GetTimeout(job *cron.Job) time.Duration {
	if ctp.TimeoutFunc != nil {
		return ctp.TimeoutFunc(job)
	}
	return 30 * time.Minute
}

func (ctp *CustomTimeoutPolicy) GetGracePeriod(job *cron.Job) time.Duration {
	if ctp.GraceFunc != nil {
		return ctp.GraceFunc(job)
	}
	return 30 * time.Second
}

func (ctp *CustomTimeoutPolicy) ShouldAlert(elapsed time.Duration, timeout time.Duration) bool {
	if ctp.AlertFunc != nil {
		return ctp.AlertFunc(elapsed, timeout)
	}
	return elapsed > timeout*8/10 && elapsed < timeout
}

func (ctp *CustomTimeoutPolicy) OnTimeout(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	if ctp.OnTimeoutFunc != nil {
		return ctp.OnTimeoutFunc(ctx, job, execution)
	}
	return nil
}

func (ctp *CustomTimeoutPolicy) OnGrace(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	if ctp.OnGraceFunc != nil {
		return ctp.OnGraceFunc(ctx, job, execution)
	}
	return nil
}

func (ctp *CustomTimeoutPolicy) OnKill(ctx context.Context, job *cron.Job, execution *cron.JobExecution) error {
	if ctp.OnKillFunc != nil {
		return ctp.OnKillFunc(ctx, job, execution)
	}
	return nil
}

func (ctp *CustomTimeoutPolicy) Name() string {
	if ctp.PolicyName != "" {
		return ctp.PolicyName
	}
	return "custom"
}
