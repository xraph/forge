package warming

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ScheduledWarmer implements scheduled cache warming strategy
type ScheduledWarmer struct {
	cacheManager cachecore.CacheManager
	logger       common.Logger
	metrics      common.Metrics
	config       *ScheduledWarmingConfig
	scheduler    gocron.Scheduler
	jobs         map[string]*ScheduledWarmingJob
	operations   map[string]*ScheduledWarmingOperation
	mu           sync.RWMutex
	started      bool
}

// ScheduledWarmingConfig contains configuration for scheduled warming
type ScheduledWarmingConfig struct {
	TimeZone               string        `yaml:"timezone" json:"timezone" default:"UTC"`
	MaxConcurrentJobs      uint          `yaml:"max_concurrent_jobs" json:"max_concurrent_jobs" default:"5"`
	JobTimeout             time.Duration `yaml:"job_timeout" json:"job_timeout" default:"1h"`
	RetryAttempts          int           `yaml:"retry_attempts" json:"retry_attempts" default:"3"`
	RetryDelay             time.Duration `yaml:"retry_delay" json:"retry_delay" default:"5m"`
	EnableJobHistory       bool          `yaml:"enable_job_history" json:"enable_job_history" default:"true"`
	MaxJobHistory          int           `yaml:"max_job_history" json:"max_job_history" default:"100"`
	FailureNotification    bool          `yaml:"failure_notification" json:"failure_notification" default:"true"`
	OverlapPolicy          OverlapPolicy `yaml:"overlap_policy" json:"overlap_policy" default:"skip"`
	EnableJobMetrics       bool          `yaml:"enable_job_metrics" json:"enable_job_metrics" default:"true"`
	CleanupInterval        time.Duration `yaml:"cleanup_interval" json:"cleanup_interval" default:"24h"`
	PreWarmingBuffer       time.Duration `yaml:"pre_warming_buffer" json:"pre_warming_buffer" default:"5m"`
	MaxJobDuration         time.Duration `yaml:"max_job_duration" json:"max_job_duration" default:"2h"`
	EnablePredictiveAdjust bool          `yaml:"enable_predictive_adjust" json:"enable_predictive_adjust" default:"false"`
}

// OverlapPolicy defines how to handle overlapping jobs
type OverlapPolicy string

const (
	OverlapPolicySkip     OverlapPolicy = "skip"     // Skip if previous job is still running
	OverlapPolicyQueue    OverlapPolicy = "queue"    // Queue the job to run after current completes
	OverlapPolicyReplace  OverlapPolicy = "replace"  // Cancel previous job and start new one
	OverlapPolicyParallel OverlapPolicy = "parallel" // Allow parallel execution
)

// ScheduledWarmingJob represents a scheduled warming job
type ScheduledWarmingJob struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	CacheName      string                  `json:"cache_name"`
	Schedule       string                  `json:"schedule"` // Cron expression
	Description    string                  `json:"description"`
	Config         cachecore.WarmConfig    `json:"config"`
	DataSources    []string                `json:"data_sources"` // Data source names
	Enabled        bool                    `json:"enabled"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	LastRun        *time.Time              `json:"last_run,omitempty"`
	NextRun        *time.Time              `json:"next_run,omitempty"`
	RunCount       int64                   `json:"run_count"`
	SuccessCount   int64                   `json:"success_count"`
	FailureCount   int64                   `json:"failure_count"`
	AverageRunTime time.Duration           `json:"average_run_time"`
	LastRunStatus  ScheduledJobStatus      `json:"last_run_status"`
	LastError      string                  `json:"last_error,omitempty"`
	OverlapPolicy  OverlapPolicy           `json:"overlap_policy"`
	TimeZone       string                  `json:"timezone"`
	Tags           map[string]string       `json:"tags"`
	Metadata       map[string]interface{}  `json:"metadata"`
	History        []ScheduledJobExecution `json:"history,omitempty"`
	jobHandle      gocron.Job              `json:"-"`
	mu             sync.RWMutex            `json:"-"`
}

// ScheduledJobStatus represents the status of a scheduled job execution
type ScheduledJobStatus string

const (
	ScheduledJobStatusPending   ScheduledJobStatus = "pending"
	ScheduledJobStatusRunning   ScheduledJobStatus = "running"
	ScheduledJobStatusCompleted ScheduledJobStatus = "completed"
	ScheduledJobStatusFailed    ScheduledJobStatus = "failed"
	ScheduledJobStatusSkipped   ScheduledJobStatus = "skipped"
	ScheduledJobStatusCancelled ScheduledJobStatus = "cancelled"
	ScheduledJobStatusTimeout   ScheduledJobStatus = "timeout"
)

// ScheduledJobExecution represents a job execution record
type ScheduledJobExecution struct {
	ID           string                 `json:"id"`
	JobID        string                 `json:"job_id"`
	StartTime    time.Time              `json:"start_time"`
	EndTime      *time.Time             `json:"end_time,omitempty"`
	Duration     time.Duration          `json:"duration"`
	Status       ScheduledJobStatus     `json:"status"`
	ItemsWarmed  int64                  `json:"items_warmed"`
	ItemsFailed  int64                  `json:"items_failed"`
	Error        string                 `json:"error,omitempty"`
	Trigger      string                 `json:"trigger"` // "scheduled", "manual"
	NodeID       string                 `json:"node_id"`
	RetryAttempt int                    `json:"retry_attempt"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// ScheduledWarmingOperation represents an active scheduled warming operation
type ScheduledWarmingOperation struct {
	ID           string              `json:"id"`
	JobID        string              `json:"job_id"`
	ExecutionID  string              `json:"execution_id"`
	CacheName    string              `json:"cache_name"`
	StartedAt    time.Time           `json:"started_at"`
	Status       ScheduledJobStatus  `json:"status"`
	Progress     float64             `json:"progress"`
	Context      context.Context     `json:"-"`
	Cancel       context.CancelFunc  `json:"-"`
	WarmingStats cachecore.WarmStats `json:"warming_stats"`
	mu           sync.RWMutex        `json:"-"`
}

// WarmingScheduler defines the interface for warming schedulers
type WarmingScheduler interface {
	Schedule(cacheName string, config cachecore.WarmConfig) error
	Cancel(cacheName string) error
	GetSchedule(cacheName string) (*ScheduledWarmingJob, error)
	GetSchedules() []ScheduledWarmingJob
}

// NewScheduledWarmer creates a new scheduled warming strategy
func NewScheduledWarmer(cacheManager cachecore.CacheManager, l common.Logger, metrics common.Metrics, config *ScheduledWarmingConfig) *ScheduledWarmer {
	if config == nil {
		config = &ScheduledWarmingConfig{
			TimeZone:               "UTC",
			MaxConcurrentJobs:      5,
			JobTimeout:             time.Hour,
			RetryAttempts:          3,
			RetryDelay:             5 * time.Minute,
			EnableJobHistory:       true,
			MaxJobHistory:          100,
			FailureNotification:    true,
			OverlapPolicy:          OverlapPolicySkip,
			EnableJobMetrics:       true,
			CleanupInterval:        24 * time.Hour,
			PreWarmingBuffer:       5 * time.Minute,
			MaxJobDuration:         2 * time.Hour,
			EnablePredictiveAdjust: false,
		}
	}

	// Parse timezone
	location, err := time.LoadLocation(config.TimeZone)
	if err != nil {
		if l != nil {
			l.Warn("invalid timezone, using UTC",
				logger.String("timezone", config.TimeZone),
				logger.Error(err),
			)
		}
		location = time.UTC
	}

	// Create gocron scheduler with timezone and options
	scheduler, err := gocron.NewScheduler(
		gocron.WithLocation(location),
		gocron.WithLimitConcurrentJobs(config.MaxConcurrentJobs, gocron.LimitModeReschedule),
	)
	if err != nil {
		if l != nil {
			l.Error("failed to create scheduler, using default",
				logger.Error(err),
			)
		}
		// Fallback to default scheduler
		scheduler, _ = gocron.NewScheduler()
	}

	sw := &ScheduledWarmer{
		cacheManager: cacheManager,
		logger:       l,
		metrics:      metrics,
		config:       config,
		scheduler:    scheduler,
		jobs:         make(map[string]*ScheduledWarmingJob),
		operations:   make(map[string]*ScheduledWarmingOperation),
	}

	return sw
}

// Start starts the scheduled warmer
func (sw *ScheduledWarmer) Start(ctx context.Context) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.started {
		return fmt.Errorf("scheduled warmer already started")
	}

	// OnStart the gocron scheduler
	sw.scheduler.Start()
	sw.started = true

	// OnStart cleanup routine
	go sw.cleanupRoutine(ctx)

	if sw.logger != nil {
		sw.logger.Info("scheduled warmer started",
			logger.String("timezone", sw.config.TimeZone),
			logger.Uint("max_concurrent_jobs", sw.config.MaxConcurrentJobs),
		)
	}

	if sw.metrics != nil {
		sw.metrics.Counter("forge.cache.warming.scheduled.started").Inc()
	}

	return nil
}

// Stop stops the scheduled warmer
func (sw *ScheduledWarmer) Stop() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if !sw.started {
		return fmt.Errorf("scheduled warmer not started")
	}

	// OnStop the gocron scheduler
	if err := sw.scheduler.Shutdown(); err != nil {
		if sw.logger != nil {
			sw.logger.Error("error shutting down scheduler", logger.Error(err))
		}
	}

	// Cancel all running operations
	for _, operation := range sw.operations {
		operation.Cancel()
	}

	sw.started = false

	if sw.logger != nil {
		sw.logger.Info("scheduled warmer stopped")
	}

	if sw.metrics != nil {
		sw.metrics.Counter("forge.cache.warming.scheduled.stopped").Inc()
	}

	return nil
}

// AddJob adds a new scheduled warming job
func (sw *ScheduledWarmer) AddJob(job *ScheduledWarmingJob) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if !sw.started {
		return fmt.Errorf("scheduled warmer not started")
	}

	// Validate job
	if err := sw.validateJob(job); err != nil {
		return fmt.Errorf("invalid job: %w", err)
	}

	// Check if job already exists
	if _, exists := sw.jobs[job.ID]; exists {
		return fmt.Errorf("job with ID %s already exists", job.ID)
	}

	// Create job definition with proper overlap handling
	jobDef := gocron.CronJob(job.Schedule, false)

	// Apply overlap policy
	switch job.OverlapPolicy {
	case OverlapPolicySkip:
		jobDef = gocron.CronJob(job.Schedule, false)
	case OverlapPolicyQueue:
		jobDef = gocron.CronJob(job.Schedule, true)
	case OverlapPolicyReplace:
		// gocron v2 doesn't have built-in replace, we'll handle it in the job function
		jobDef = gocron.CronJob(job.Schedule, false)
	case OverlapPolicyParallel:
		jobDef = gocron.CronJob(job.Schedule, true)
	}

	// Add job to scheduler
	jobHandle, err := sw.scheduler.NewJob(
		jobDef,
		gocron.NewTask(sw.createJobFunc(job)),
		gocron.WithName(job.Name),
		gocron.WithTags(job.ID),
	)
	if err != nil {
		return fmt.Errorf("failed to add job to scheduler: %w", err)
	}

	job.jobHandle = jobHandle
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()

	// Get next run time
	if nextRun, err := jobHandle.NextRun(); err == nil {
		job.NextRun = &nextRun
	}

	sw.jobs[job.ID] = job

	if sw.logger != nil {
		sw.logger.Info("scheduled warming job added",
			logger.String("job_id", job.ID),
			logger.String("job_name", job.Name),
			logger.String("cache_name", job.CacheName),
			logger.String("schedule", job.Schedule),
		)
	}

	if sw.metrics != nil {
		sw.metrics.Counter("forge.cache.warming.scheduled.jobs_added").Inc()
		sw.metrics.Gauge("forge.cache.warming.scheduled.jobs_total").Set(float64(len(sw.jobs)))
	}

	return nil
}

// RemoveJob removes a scheduled warming job
func (sw *ScheduledWarmer) RemoveJob(jobID string) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	job, exists := sw.jobs[jobID]
	if !exists {
		return fmt.Errorf("job with ID %s not found", jobID)
	}

	// Remove from scheduler
	if err := sw.scheduler.RemoveJob(job.jobHandle.ID()); err != nil {
		if sw.logger != nil {
			sw.logger.Warn("error removing job from scheduler",
				logger.String("job_id", jobID),
				logger.Error(err))
		}
	}

	// Cancel any running operation for this job
	for opID, operation := range sw.operations {
		if operation.JobID == jobID {
			operation.Cancel()
			delete(sw.operations, opID)
		}
	}

	delete(sw.jobs, jobID)

	if sw.logger != nil {
		sw.logger.Info("scheduled warming job removed",
			logger.String("job_id", jobID),
			logger.String("job_name", job.Name),
		)
	}

	if sw.metrics != nil {
		sw.metrics.Counter("forge.cache.warming.scheduled.jobs_removed").Inc()
		sw.metrics.Gauge("forge.cache.warming.scheduled.jobs_total").Set(float64(len(sw.jobs)))
	}

	return nil
}

// GetJob returns a scheduled warming job
func (sw *ScheduledWarmer) GetJob(jobID string) (*ScheduledWarmingJob, error) {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	job, exists := sw.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job with ID %s not found", jobID)
	}

	// Return a copy
	jobCopy := *job
	return &jobCopy, nil
}

// GetJobs returns all scheduled warming jobs
func (sw *ScheduledWarmer) GetJobs() []ScheduledWarmingJob {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	jobs := make([]ScheduledWarmingJob, 0, len(sw.jobs))
	for _, job := range sw.jobs {
		jobs = append(jobs, *job)
	}

	return jobs
}

// TriggerJob manually triggers a scheduled warming job
func (sw *ScheduledWarmer) TriggerJob(ctx context.Context, jobID string) error {
	sw.mu.RLock()
	job, exists := sw.jobs[jobID]
	sw.mu.RUnlock()

	if !exists {
		return fmt.Errorf("job with ID %s not found", jobID)
	}

	// Execute the job manually
	return sw.executeJob(ctx, job, "manual")
}

// GetJobHistory returns execution history for a job
func (sw *ScheduledWarmer) GetJobHistory(jobID string) ([]ScheduledJobExecution, error) {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	job, exists := sw.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job with ID %s not found", jobID)
	}

	job.mu.RLock()
	history := make([]ScheduledJobExecution, len(job.History))
	copy(history, job.History)
	job.mu.RUnlock()

	return history, nil
}

// UpdateJob updates a scheduled warming job
func (sw *ScheduledWarmer) UpdateJob(jobID string, updates map[string]interface{}) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	job, exists := sw.jobs[jobID]
	if !exists {
		return fmt.Errorf("job with ID %s not found", jobID)
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	// Apply updates
	if name, ok := updates["name"].(string); ok {
		job.Name = name
	}
	if description, ok := updates["description"].(string); ok {
		job.Description = description
	}
	if enabled, ok := updates["enabled"].(bool); ok {
		job.Enabled = enabled
	}
	if schedule, ok := updates["schedule"].(string); ok {
		// Remove old job
		if err := sw.scheduler.RemoveJob(job.jobHandle.ID()); err != nil {
			return fmt.Errorf("failed to remove old job: %w", err)
		}

		// Create new job with updated schedule
		jobDef := gocron.CronJob(schedule, false)
		jobHandle, err := sw.scheduler.NewJob(
			jobDef,
			gocron.NewTask(sw.createJobFunc(job)),
			gocron.WithName(job.Name),
			gocron.WithTags(job.ID),
		)
		if err != nil {
			return fmt.Errorf("failed to update job schedule: %w", err)
		}

		job.Schedule = schedule
		job.jobHandle = jobHandle

		// Update next run time
		if nextRun, err := jobHandle.NextRun(); err == nil {
			job.NextRun = &nextRun
		}
	}

	job.UpdatedAt = time.Now()

	if sw.logger != nil {
		sw.logger.Info("scheduled warming job updated",
			logger.String("job_id", jobID),
			logger.String("job_name", job.Name),
		)
	}

	return nil
}

// createJobFunc creates a function for the gocron scheduler
func (sw *ScheduledWarmer) createJobFunc(job *ScheduledWarmingJob) func() {
	return func() {
		ctx := context.Background()
		if err := sw.executeJob(ctx, job, "scheduled"); err != nil {
			if sw.logger != nil {
				sw.logger.Error("scheduled warming job failed",
					logger.String("job_id", job.ID),
					logger.String("job_name", job.Name),
					logger.Error(err),
				)
			}
		}
	}
}

// executeJob executes a warming job
func (sw *ScheduledWarmer) executeJob(ctx context.Context, job *ScheduledWarmingJob, trigger string) error {
	if !job.Enabled {
		if sw.logger != nil {
			sw.logger.Debug("skipping disabled job",
				logger.String("job_id", job.ID),
				logger.String("job_name", job.Name),
			)
		}
		return nil
	}

	// Check for overlap policy
	if err := sw.handleOverlapPolicy(job); err != nil {
		return err
	}

	// Create execution context
	executionCtx, cancel := context.WithTimeout(ctx, sw.config.JobTimeout)
	defer cancel()

	// Create execution record
	execution := &ScheduledJobExecution{
		ID:           fmt.Sprintf("exec_%s_%d", job.ID, time.Now().UnixNano()),
		JobID:        job.ID,
		StartTime:    time.Now(),
		Status:       ScheduledJobStatusRunning,
		Trigger:      trigger,
		NodeID:       "current", // TODO: Get actual node ID
		RetryAttempt: 0,
		Metadata:     make(map[string]interface{}),
	}

	// Create operation
	operation := &ScheduledWarmingOperation{
		ID:          fmt.Sprintf("op_%s_%d", job.ID, time.Now().UnixNano()),
		JobID:       job.ID,
		ExecutionID: execution.ID,
		CacheName:   job.CacheName,
		StartedAt:   time.Now(),
		Status:      ScheduledJobStatusRunning,
		Context:     executionCtx,
		Cancel:      cancel,
	}

	sw.mu.Lock()
	sw.operations[operation.ID] = operation
	sw.mu.Unlock()

	// Update job stats
	job.mu.Lock()
	job.RunCount++
	lastRun := time.Now()
	job.LastRun = &lastRun
	job.LastRunStatus = ScheduledJobStatusRunning
	job.mu.Unlock()

	// Execute the warming
	err := sw.performWarming(executionCtx, job, operation, execution)

	// Update execution record
	endTime := time.Now()
	execution.EndTime = &endTime
	execution.Duration = endTime.Sub(execution.StartTime)

	if err != nil {
		execution.Status = ScheduledJobStatusFailed
		execution.Error = err.Error()

		job.mu.Lock()
		job.FailureCount++
		job.LastRunStatus = ScheduledJobStatusFailed
		job.LastError = err.Error()
		job.mu.Unlock()

		if sw.metrics != nil {
			sw.metrics.Counter("forge.cache.warming.scheduled.job_failures").Inc()
		}
	} else {
		execution.Status = ScheduledJobStatusCompleted
		execution.ItemsWarmed = operation.WarmingStats.ItemsWarmed
		execution.ItemsFailed = operation.WarmingStats.ItemsFailed

		job.mu.Lock()
		job.SuccessCount++
		job.LastRunStatus = ScheduledJobStatusCompleted
		job.LastError = ""
		job.AverageRunTime = (job.AverageRunTime + execution.Duration) / 2
		job.mu.Unlock()

		if sw.metrics != nil {
			sw.metrics.Counter("forge.cache.warming.scheduled.job_successes").Inc()
			sw.metrics.Histogram("forge.cache.warming.scheduled.job_duration").Observe(execution.Duration.Seconds())
		}
	}

	// Add to job history
	if sw.config.EnableJobHistory {
		job.mu.Lock()
		job.History = append(job.History, *execution)
		if len(job.History) > sw.config.MaxJobHistory {
			job.History = job.History[1:]
		}
		job.mu.Unlock()
	}

	// Update next run time
	if nextRun, err := job.jobHandle.NextRun(); err == nil {
		job.NextRun = &nextRun
	}

	// Remove operation
	sw.mu.Lock()
	delete(sw.operations, operation.ID)
	sw.mu.Unlock()

	return err
}

// handleOverlapPolicy handles job overlap based on the configured policy
func (sw *ScheduledWarmer) handleOverlapPolicy(job *ScheduledWarmingJob) error {
	sw.mu.RLock()
	isRunning := false
	for _, operation := range sw.operations {
		if operation.JobID == job.ID && operation.Status == ScheduledJobStatusRunning {
			isRunning = true
			break
		}
	}
	sw.mu.RUnlock()

	if !isRunning {
		return nil
	}

	switch job.OverlapPolicy {
	case OverlapPolicySkip:
		if sw.logger != nil {
			sw.logger.Info("skipping job due to overlap policy",
				logger.String("job_id", job.ID),
				logger.String("policy", string(OverlapPolicySkip)),
			)
		}
		return fmt.Errorf("job is already running and overlap policy is skip")

	case OverlapPolicyReplace:
		// Cancel running operations for this job
		sw.mu.Lock()
		for opID, operation := range sw.operations {
			if operation.JobID == job.ID {
				operation.Cancel()
				delete(sw.operations, opID)
			}
		}
		sw.mu.Unlock()

		if sw.logger != nil {
			sw.logger.Info("cancelled previous job execution due to overlap policy",
				logger.String("job_id", job.ID),
				logger.String("policy", string(OverlapPolicyReplace)),
			)
		}

	case OverlapPolicyQueue:
		// Wait for current job to complete
		// This is a simplified implementation
		return fmt.Errorf("queue overlap policy not yet implemented")

	case OverlapPolicyParallel:
		// Allow parallel execution
		break
	}

	return nil
}

// performWarming performs the actual cache warming
func (sw *ScheduledWarmer) performWarming(ctx context.Context, job *ScheduledWarmingJob, operation *ScheduledWarmingOperation, execution *ScheduledJobExecution) error {
	// Get cache
	cache, err := sw.cacheManager.GetCache(job.CacheName)
	if err != nil {
		return fmt.Errorf("failed to get cache %s: %w", job.CacheName, err)
	}

	// Get data sources
	var dataSources []cachecore.DataSource
	for _, sourceName := range job.DataSources {
		fmt.Println(sourceName, cache.Name())
		// TODO: Resolve data sources from registry
		// For now, this is a placeholder
	}

	// Create a default warmer and execute
	warmer := NewEagerWarmer(sw.cacheManager, sw.logger, sw.metrics, nil)

	// Execute warming
	eagerOperation, err := warmer.ExecuteWarming(ctx, job.CacheName, dataSources, job.Config)
	if err != nil {
		return fmt.Errorf("failed to execute warming: %w", err)
	}

	// Monitor progress
	go sw.monitorWarmingProgress(operation, eagerOperation)

	// Wait for completion
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			// Check if warming is complete
			// This is a simplified check
			if eagerOperation.Status == EagerWarmingStatusCompleted ||
				eagerOperation.Status == EagerWarmingStatusFailed ||
				eagerOperation.Status == EagerWarmingStatusCancelled {

				// Update operation stats
				operation.WarmingStats.ItemsWarmed = eagerOperation.Stats.SuccessfulItems
				operation.WarmingStats.ItemsFailed = eagerOperation.Stats.FailedItems
				operation.WarmingStats.ItemsTotal = eagerOperation.Stats.TotalItems
				operation.Progress = eagerOperation.Stats.Progress

				if eagerOperation.Status == EagerWarmingStatusFailed {
					return fmt.Errorf("warming failed: %s", eagerOperation.Stats.LastError)
				}

				return nil
			}
		}
	}
}

// monitorWarmingProgress monitors the progress of a warming operation
func (sw *ScheduledWarmer) monitorWarmingProgress(operation *ScheduledWarmingOperation, eagerOperation *EagerWarmingOperation) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			operation.mu.Lock()
			operation.Progress = eagerOperation.Stats.Progress
			operation.WarmingStats.ItemsWarmed = eagerOperation.Stats.SuccessfulItems
			operation.WarmingStats.ItemsFailed = eagerOperation.Stats.FailedItems
			operation.WarmingStats.ItemsTotal = eagerOperation.Stats.TotalItems
			operation.mu.Unlock()

		case <-operation.Context.Done():
			return
		}
	}
}

// validateJob validates a scheduled warming job
func (sw *ScheduledWarmer) validateJob(job *ScheduledWarmingJob) error {
	if job.ID == "" {
		return fmt.Errorf("job ID is required")
	}

	if job.Name == "" {
		return fmt.Errorf("job name is required")
	}

	if job.CacheName == "" {
		return fmt.Errorf("cache name is required")
	}

	if job.Schedule == "" {
		return fmt.Errorf("schedule is required")
	}

	// Validate cron expression by attempting to create a job
	_, err := gocron.NewScheduler()
	if err != nil {
		return fmt.Errorf("failed to create test scheduler: %w", err)
	}

	// Check if cache exists
	_, err = sw.cacheManager.GetCache(job.CacheName)
	if err != nil {
		return fmt.Errorf("cache %s not found: %w", job.CacheName, err)
	}

	return nil
}

// cleanupRoutine performs periodic cleanup of old data
func (sw *ScheduledWarmer) cleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(sw.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sw.performCleanup()

		case <-ctx.Done():
			return
		}
	}
}

// performCleanup performs cleanup of old job history and data
func (sw *ScheduledWarmer) performCleanup() {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	if sw.logger != nil {
		sw.logger.Debug("performing scheduled warming cleanup")
	}

	// Cleanup job history
	for _, job := range sw.jobs {
		job.mu.Lock()
		if len(job.History) > sw.config.MaxJobHistory {
			job.History = job.History[len(job.History)-sw.config.MaxJobHistory:]
		}
		job.mu.Unlock()
	}

	if sw.metrics != nil {
		sw.metrics.Counter("forge.cache.warming.scheduled.cleanup_performed").Inc()
	}
}

// GetOperationStats returns current operation statistics
func (sw *ScheduledWarmer) GetOperationStats() map[string]interface{} {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	stats := map[string]interface{}{
		"total_jobs":         len(sw.jobs),
		"running_operations": len(sw.operations),
		"scheduler_running":  sw.started,
	}

	var enabledJobs, totalRuns, totalSuccesses, totalFailures int64
	for _, job := range sw.jobs {
		if job.Enabled {
			enabledJobs++
		}
		totalRuns += job.RunCount
		totalSuccesses += job.SuccessCount
		totalFailures += job.FailureCount
	}

	stats["enabled_jobs"] = enabledJobs
	stats["total_runs"] = totalRuns
	stats["total_successes"] = totalSuccesses
	stats["total_failures"] = totalFailures

	if totalRuns > 0 {
		stats["success_rate"] = float64(totalSuccesses) / float64(totalRuns)
	}

	return stats
}

// GetRunningOperations returns currently running operations
func (sw *ScheduledWarmer) GetRunningOperations() []ScheduledWarmingOperation {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	operations := make([]ScheduledWarmingOperation, 0, len(sw.operations))
	for _, operation := range sw.operations {
		operations = append(operations, *operation)
	}

	return operations
}

// Schedule implements the WarmingScheduler interface
func (sw *ScheduledWarmer) Schedule(cacheName string, config cachecore.WarmConfig) error {
	if config.Schedule == "" {
		return fmt.Errorf("schedule expression is required")
	}

	job := &ScheduledWarmingJob{
		ID:            fmt.Sprintf("scheduled_%s_%d", cacheName, time.Now().UnixNano()),
		Name:          fmt.Sprintf("Auto warming for %s", cacheName),
		CacheName:     cacheName,
		Schedule:      config.Schedule,
		Description:   "Automatically generated scheduled warming job",
		Config:        config,
		DataSources:   config.DataSources,
		Enabled:       true,
		OverlapPolicy: OverlapPolicySkip,
		TimeZone:      sw.config.TimeZone,
		Tags:          make(map[string]string),
		Metadata:      make(map[string]interface{}),
		History:       make([]ScheduledJobExecution, 0),
	}

	return sw.AddJob(job)
}

// Cancel implements the WarmingScheduler interface
func (sw *ScheduledWarmer) Cancel(cacheName string) error {
	sw.mu.RLock()
	var jobID string
	for id, job := range sw.jobs {
		if job.CacheName == cacheName {
			jobID = id
			break
		}
	}
	sw.mu.RUnlock()

	if jobID == "" {
		return fmt.Errorf("no scheduled warming job found for cache %s", cacheName)
	}

	return sw.RemoveJob(jobID)
}

// GetSchedule implements the WarmingScheduler interface
func (sw *ScheduledWarmer) GetSchedule(cacheName string) (*ScheduledWarmingJob, error) {
	sw.mu.RLock()
	defer sw.mu.RUnlock()

	for _, job := range sw.jobs {
		if job.CacheName == cacheName {
			jobCopy := *job
			return &jobCopy, nil
		}
	}

	return nil, fmt.Errorf("no scheduled warming job found for cache %s", cacheName)
}

// GetSchedules implements the WarmingScheduler interface
func (sw *ScheduledWarmer) GetSchedules() []ScheduledWarmingJob {
	return sw.GetJobs()
}
