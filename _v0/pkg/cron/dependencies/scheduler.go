package dependencies

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	cron "github.com/xraph/forge/v0/pkg/cron/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// DependencyAwareScheduler provides intelligent job scheduling based on dependencies
type DependencyAwareScheduler struct {
	// Core components
	resolver      *DependencyResolver
	baseScheduler SchedulerInterface

	// Scheduling state
	scheduledJobs map[string]*ScheduledJobInfo
	executionPlan *ExecutionPlan

	// Configuration
	config *SchedulerConfig

	// Concurrency control
	mu sync.RWMutex

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// SchedulerInterface defines the interface for base scheduler integration
type SchedulerInterface interface {
	ScheduleJob(job *cron.Job) error
	UnscheduleJob(jobID string) error
	GetDueJobs() []*cron.Job
	GetJobSchedule(jobID string) (time.Time, error)
	UpdateJobNextRun(jobID string, nextRun time.Time) error
}

// SchedulerConfig contains configuration for the dependency-aware scheduler
type SchedulerConfig struct {
	MaxConcurrentJobs    int           `json:"max_concurrent_jobs" yaml:"max_concurrent_jobs"`
	DependencyTimeout    time.Duration `json:"dependency_timeout" yaml:"dependency_timeout"`
	RetryOnFailure       bool          `json:"retry_on_failure" yaml:"retry_on_failure"`
	MaxRetries           int           `json:"max_retries" yaml:"max_retries"`
	RetryBackoff         time.Duration `json:"retry_backoff" yaml:"retry_backoff"`
	SkipFailedDeps       bool          `json:"skip_failed_deps" yaml:"skip_failed_deps"`
	ParallelExecution    bool          `json:"parallel_execution" yaml:"parallel_execution"`
	DeadlineHandling     string        `json:"deadline_handling" yaml:"deadline_handling"` // "skip", "delay", "force"
	PriorityScheduling   bool          `json:"priority_scheduling" yaml:"priority_scheduling"`
	OptimisticScheduling bool          `json:"optimistic_scheduling" yaml:"optimistic_scheduling"`
}

// ScheduledJobInfo contains information about a scheduled job
type ScheduledJobInfo struct {
	Job            *cron.Job
	ScheduledAt    time.Time
	NextRun        time.Time
	Dependencies   []string
	Dependents     []string
	ExecutionLevel int
	Priority       int
	Status         SchedulingStatus
	RetryCount     int
	LastError      error
	DeadlineTime   time.Time
}

// SchedulingStatus represents the scheduling status of a job
type SchedulingStatus string

const (
	SchedulingStatusPending         SchedulingStatus = "pending"
	SchedulingStatusReady           SchedulingStatus = "ready"
	SchedulingStatusWaiting         SchedulingStatus = "waiting"
	SchedulingStatusRunning         SchedulingStatus = "running"
	SchedulingStatusCompleted       SchedulingStatus = "completed"
	SchedulingStatusFailed          SchedulingStatus = "failed"
	SchedulingStatusSkipped         SchedulingStatus = "skipped"
	SchedulingStatusDeadlineReached SchedulingStatus = "deadline_reached"
)

// ExecutionPlan represents a planned execution of jobs
type ExecutionPlan struct {
	ID              string
	Jobs            []string
	ExecutionLevels [][]string
	EstimatedTime   time.Duration
	CreatedAt       time.Time
	Status          PlanStatus
}

// PlanStatus represents the status of an execution plan
type PlanStatus string

const (
	PlanStatusPending   PlanStatus = "pending"
	PlanStatusExecuting PlanStatus = "executing"
	PlanStatusCompleted PlanStatus = "completed"
	PlanStatusFailed    PlanStatus = "failed"
	PlanStatusCancelled PlanStatus = "cancelled"
)

// NewDependencyAwareScheduler creates a new dependency-aware scheduler
func NewDependencyAwareScheduler(
	resolver *DependencyResolver,
	baseScheduler SchedulerInterface,
	config *SchedulerConfig,
	logger common.Logger,
	metrics common.Metrics,
) *DependencyAwareScheduler {
	return &DependencyAwareScheduler{
		resolver:      resolver,
		baseScheduler: baseScheduler,
		config:        config,
		scheduledJobs: make(map[string]*ScheduledJobInfo),
		logger:        logger,
		metrics:       metrics,
	}
}

// ScheduleJob schedules a job with dependency awareness
func (das *DependencyAwareScheduler) ScheduleJob(job *cron.Job) error {
	das.mu.Lock()
	defer das.mu.Unlock()

	if job == nil {
		return common.ErrValidationError("job", fmt.Errorf("job cannot be nil"))
	}

	jobID := job.Definition.ID

	// Check if job already scheduled
	if _, exists := das.scheduledJobs[jobID]; exists {
		return common.ErrServiceAlreadyExists(jobID)
	}

	// Register job with dependency resolver
	if err := das.resolver.RegisterJob(job); err != nil {
		return err
	}

	// Create scheduled job info
	scheduledInfo := &ScheduledJobInfo{
		Job:            job,
		ScheduledAt:    time.Now(),
		NextRun:        job.NextRun,
		Dependencies:   job.Definition.Dependencies,
		Dependents:     das.resolver.GetJobDependents(jobID),
		ExecutionLevel: das.getJobExecutionLevel(jobID),
		Priority:       job.Definition.Priority,
		Status:         SchedulingStatusPending,
		RetryCount:     0,
	}

	// Set deadline if applicable
	if job.Definition.Timeout > 0 {
		scheduledInfo.DeadlineTime = scheduledInfo.NextRun.Add(job.Definition.Timeout)
	}

	das.scheduledJobs[jobID] = scheduledInfo

	// Schedule with base scheduler if no dependencies or optimistic scheduling
	if len(job.Definition.Dependencies) == 0 || das.config.OptimisticScheduling {
		if err := das.baseScheduler.ScheduleJob(job); err != nil {
			das.resolver.UnregisterJob(jobID)
			delete(das.scheduledJobs, jobID)
			return err
		}
	}

	if das.logger != nil {
		das.logger.Info("job scheduled with dependency awareness",
			logger.String("job_id", jobID),
			logger.String("dependencies", fmt.Sprintf("%v", job.Definition.Dependencies)),
			logger.Int("execution_level", scheduledInfo.ExecutionLevel),
			logger.Int("priority", scheduledInfo.Priority),
		)
	}

	if das.metrics != nil {
		das.metrics.Counter("forge.cron.dependency_scheduler.jobs_scheduled").Inc()
	}

	return nil
}

// UnscheduleJob removes a job from the scheduler
func (das *DependencyAwareScheduler) UnscheduleJob(jobID string) error {
	das.mu.Lock()
	defer das.mu.Unlock()

	_, exists := das.scheduledJobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Unregister from dependency resolver
	if err := das.resolver.UnregisterJob(jobID); err != nil {
		return err
	}

	// Unschedule from base scheduler
	if err := das.baseScheduler.UnscheduleJob(jobID); err != nil {
		// Log error but don't fail
		if das.logger != nil {
			das.logger.Warn("failed to unschedule job from base scheduler",
				logger.String("job_id", jobID),
				logger.Error(err),
			)
		}
	}

	delete(das.scheduledJobs, jobID)

	if das.logger != nil {
		das.logger.Info("job unscheduled", logger.String("job_id", jobID))
	}

	if das.metrics != nil {
		das.metrics.Counter("forge.cron.dependency_scheduler.jobs_unscheduled").Inc()
	}

	return nil
}

// GetReadyJobs returns jobs that are ready to execute based on dependencies
func (das *DependencyAwareScheduler) GetReadyJobs(ctx context.Context) ([]*cron.Job, error) {
	das.mu.RLock()
	defer das.mu.RUnlock()

	var readyJobs []*cron.Job
	now := time.Now()

	// Get due jobs from base scheduler
	dueJobs := das.baseScheduler.GetDueJobs()
	dueJobIDs := make(map[string]bool)
	for _, job := range dueJobs {
		dueJobIDs[job.Definition.ID] = true
	}

	// Check dependency resolution for each scheduled job
	for jobID, scheduledInfo := range das.scheduledJobs {
		// Skip if not due yet (unless optimistic scheduling)
		if !dueJobIDs[jobID] && !das.config.OptimisticScheduling {
			continue
		}

		// Skip if already running or completed
		if scheduledInfo.Status == SchedulingStatusRunning ||
			scheduledInfo.Status == SchedulingStatusCompleted {
			continue
		}

		// Check deadline
		if !scheduledInfo.DeadlineTime.IsZero() && now.After(scheduledInfo.DeadlineTime) {
			das.handleDeadlineReached(scheduledInfo)
			continue
		}

		// Resolve dependencies
		resolution, err := das.resolver.ResolveJobDependencies(ctx, jobID)
		if err != nil {
			if das.logger != nil {
				das.logger.Error("failed to resolve job dependencies",
					logger.String("job_id", jobID),
					logger.Error(err),
				)
			}
			continue
		}

		// Check if job can execute
		if resolution.CanExecute {
			scheduledInfo.Status = SchedulingStatusReady
			readyJobs = append(readyJobs, scheduledInfo.Job)
		} else {
			// Handle failed dependencies
			if len(resolution.FailedDeps) > 0 {
				if das.config.SkipFailedDeps {
					scheduledInfo.Status = SchedulingStatusSkipped
					scheduledInfo.LastError = fmt.Errorf("skipped due to failed dependencies: %v", resolution.FailedDeps)

					if das.logger != nil {
						das.logger.Warn("job skipped due to failed dependencies",
							logger.String("job_id", jobID),
							logger.String("failed_deps", fmt.Sprintf("%v", resolution.FailedDeps)),
						)
					}
				} else {
					scheduledInfo.Status = SchedulingStatusFailed
					scheduledInfo.LastError = fmt.Errorf("dependencies failed: %v", resolution.FailedDeps)
				}
			} else {
				scheduledInfo.Status = SchedulingStatusWaiting
			}
		}
	}

	// Sort ready jobs by priority and execution level
	if das.config.PriorityScheduling {
		sort.Slice(readyJobs, func(i, j int) bool {
			jobI := das.scheduledJobs[readyJobs[i].Definition.ID]
			jobJ := das.scheduledJobs[readyJobs[j].Definition.ID]

			// Sort by priority first (higher priority first)
			if jobI.Priority != jobJ.Priority {
				return jobI.Priority > jobJ.Priority
			}

			// Then by execution level (lower level first)
			return jobI.ExecutionLevel < jobJ.ExecutionLevel
		})
	}

	// Limit concurrent jobs
	if das.config.MaxConcurrentJobs > 0 && len(readyJobs) > das.config.MaxConcurrentJobs {
		readyJobs = readyJobs[:das.config.MaxConcurrentJobs]
	}

	return readyJobs, nil
}

// StartJobExecution marks a job as started
func (das *DependencyAwareScheduler) StartJobExecution(ctx context.Context, jobID string) error {
	das.mu.Lock()
	defer das.mu.Unlock()

	scheduledInfo, exists := das.scheduledJobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	scheduledInfo.Status = SchedulingStatusRunning

	// Notify dependency resolver
	if err := das.resolver.StartJobExecution(ctx, jobID); err != nil {
		return err
	}

	if das.logger != nil {
		das.logger.Info("job execution started",
			logger.String("job_id", jobID),
			logger.Int("execution_level", scheduledInfo.ExecutionLevel),
		)
	}

	if das.metrics != nil {
		das.metrics.Counter("forge.cron.dependency_scheduler.executions_started").Inc()
	}

	return nil
}

// CompleteJobExecution marks a job as completed
func (das *DependencyAwareScheduler) CompleteJobExecution(ctx context.Context, jobID string, success bool, err error) error {
	das.mu.Lock()
	defer das.mu.Unlock()

	scheduledInfo, exists := das.scheduledJobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	if success {
		scheduledInfo.Status = SchedulingStatusCompleted
		scheduledInfo.RetryCount = 0
	} else {
		scheduledInfo.Status = SchedulingStatusFailed
		scheduledInfo.LastError = err
		scheduledInfo.RetryCount++
	}

	// Notify dependency resolver
	if err := das.resolver.CompleteJobExecution(ctx, jobID, success, err); err != nil {
		return err
	}

	// Handle retry logic
	if !success && das.config.RetryOnFailure && scheduledInfo.RetryCount < das.config.MaxRetries {
		if err := das.scheduleRetry(scheduledInfo); err != nil {
			if das.logger != nil {
				das.logger.Error("failed to schedule retry",
					logger.String("job_id", jobID),
					logger.Error(err),
				)
			}
		}
	}

	// Schedule next run if successful
	if success {
		if err := das.scheduleNextRun(scheduledInfo); err != nil {
			if das.logger != nil {
				das.logger.Error("failed to schedule next run",
					logger.String("job_id", jobID),
					logger.Error(err),
				)
			}
		}
	}

	if das.logger != nil {
		das.logger.Info("job execution completed",
			logger.String("job_id", jobID),
			logger.Bool("success", success),
			logger.Int("retry_count", scheduledInfo.RetryCount),
		)
	}

	if das.metrics != nil {
		if success {
			das.metrics.Counter("forge.cron.dependency_scheduler.executions_completed").Inc()
		} else {
			das.metrics.Counter("forge.cron.dependency_scheduler.executions_failed").Inc()
		}
	}

	return nil
}

// CancelJobExecution marks a job as cancelled
func (das *DependencyAwareScheduler) CancelJobExecution(ctx context.Context, jobID string) error {
	das.mu.Lock()
	defer das.mu.Unlock()

	scheduledInfo, exists := das.scheduledJobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	scheduledInfo.Status = SchedulingStatusSkipped

	// Notify dependency resolver
	if err := das.resolver.CancelJobExecution(ctx, jobID); err != nil {
		return err
	}

	if das.logger != nil {
		das.logger.Info("job execution cancelled", logger.String("job_id", jobID))
	}

	if das.metrics != nil {
		das.metrics.Counter("forge.cron.dependency_scheduler.executions_cancelled").Inc()
	}

	return nil
}

// CreateExecutionPlan creates an execution plan for a set of jobs
func (das *DependencyAwareScheduler) CreateExecutionPlan(ctx context.Context, jobIDs []string) (*ExecutionPlan, error) {
	das.mu.RLock()
	defer das.mu.RUnlock()

	// Get execution levels from dependency graph
	graph := das.resolver.GetDependencyGraph()
	levels := graph.GetExecutionLevels()

	// Filter levels to include only requested jobs
	filteredLevels := make([][]string, 0)
	jobSet := make(map[string]bool)
	for _, jobID := range jobIDs {
		jobSet[jobID] = true
	}

	for _, level := range levels {
		filteredLevel := make([]string, 0)
		for _, jobID := range level {
			if jobSet[jobID] {
				filteredLevel = append(filteredLevel, jobID)
			}
		}
		if len(filteredLevel) > 0 {
			filteredLevels = append(filteredLevels, filteredLevel)
		}
	}

	// Estimate execution time
	estimatedTime := das.estimateExecutionTime(filteredLevels)

	plan := &ExecutionPlan{
		ID:              fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		Jobs:            jobIDs,
		ExecutionLevels: filteredLevels,
		EstimatedTime:   estimatedTime,
		CreatedAt:       time.Now(),
		Status:          PlanStatusPending,
	}

	return plan, nil
}

// GetScheduledJobs returns all scheduled jobs
func (das *DependencyAwareScheduler) GetScheduledJobs() map[string]*ScheduledJobInfo {
	das.mu.RLock()
	defer das.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]*ScheduledJobInfo)
	for jobID, info := range das.scheduledJobs {
		result[jobID] = &ScheduledJobInfo{
			Job:            info.Job,
			ScheduledAt:    info.ScheduledAt,
			NextRun:        info.NextRun,
			Dependencies:   info.Dependencies,
			Dependents:     info.Dependents,
			ExecutionLevel: info.ExecutionLevel,
			Priority:       info.Priority,
			Status:         info.Status,
			RetryCount:     info.RetryCount,
			LastError:      info.LastError,
			DeadlineTime:   info.DeadlineTime,
		}
	}

	return result
}

// GetSchedulerStats returns scheduler statistics
func (das *DependencyAwareScheduler) GetSchedulerStats() SchedulerStats {
	das.mu.RLock()
	defer das.mu.RUnlock()

	stats := SchedulerStats{
		TotalJobs:  len(das.scheduledJobs),
		LastUpdate: time.Now(),
	}

	// Count jobs by status
	statusCounts := make(map[SchedulingStatus]int)
	for _, info := range das.scheduledJobs {
		statusCounts[info.Status]++
	}

	stats.PendingJobs = statusCounts[SchedulingStatusPending]
	stats.ReadyJobs = statusCounts[SchedulingStatusReady]
	stats.WaitingJobs = statusCounts[SchedulingStatusWaiting]
	stats.RunningJobs = statusCounts[SchedulingStatusRunning]
	stats.CompletedJobs = statusCounts[SchedulingStatusCompleted]
	stats.FailedJobs = statusCounts[SchedulingStatusFailed]
	stats.SkippedJobs = statusCounts[SchedulingStatusSkipped]

	// Get dependency stats
	depStats := das.resolver.GetDependencyStats()
	stats.DependencyStats = depStats

	return stats
}

// getJobExecutionLevel calculates the execution level of a job
func (das *DependencyAwareScheduler) getJobExecutionLevel(jobID string) int {
	graph := das.resolver.GetDependencyGraph()
	levels := graph.GetExecutionLevels()

	for level, jobs := range levels {
		for _, job := range jobs {
			if job == jobID {
				return level
			}
		}
	}

	return 0
}

// handleDeadlineReached handles jobs that have reached their deadline
func (das *DependencyAwareScheduler) handleDeadlineReached(scheduledInfo *ScheduledJobInfo) {
	scheduledInfo.Status = SchedulingStatusDeadlineReached

	switch das.config.DeadlineHandling {
	case "skip":
		scheduledInfo.Status = SchedulingStatusSkipped
		scheduledInfo.LastError = fmt.Errorf("deadline reached")
	case "delay":
		// Extend deadline
		scheduledInfo.DeadlineTime = scheduledInfo.DeadlineTime.Add(das.config.DependencyTimeout)
	case "force":
		// Force execution despite dependencies
		scheduledInfo.Status = SchedulingStatusReady
	}

	if das.logger != nil {
		das.logger.Warn("job deadline reached",
			logger.String("job_id", scheduledInfo.Job.Definition.ID),
			logger.String("handling", das.config.DeadlineHandling),
		)
	}

	if das.metrics != nil {
		das.metrics.Counter("forge.cron.dependency_scheduler.deadlines_reached").Inc()
	}
}

// scheduleRetry schedules a job for retry
func (das *DependencyAwareScheduler) scheduleRetry(scheduledInfo *ScheduledJobInfo) error {
	retryDelay := das.config.RetryBackoff * time.Duration(scheduledInfo.RetryCount)
	nextRun := time.Now().Add(retryDelay)

	scheduledInfo.NextRun = nextRun
	scheduledInfo.Status = SchedulingStatusPending

	if err := das.baseScheduler.UpdateJobNextRun(scheduledInfo.Job.Definition.ID, nextRun); err != nil {
		return err
	}

	if das.logger != nil {
		das.logger.Info("job scheduled for retry",
			logger.String("job_id", scheduledInfo.Job.Definition.ID),
			logger.Int("retry_count", scheduledInfo.RetryCount),
			logger.Duration("delay", retryDelay),
		)
	}

	return nil
}

// scheduleNextRun schedules the next run of a job
func (das *DependencyAwareScheduler) scheduleNextRun(scheduledInfo *ScheduledJobInfo) error {
	// Calculate next run time based on cron schedule
	nextRun, err := calculateNextRun(scheduledInfo.Job.Definition.Schedule, time.Now())
	if err != nil {
		return err
	}

	scheduledInfo.NextRun = nextRun
	scheduledInfo.Status = SchedulingStatusPending

	if err := das.baseScheduler.UpdateJobNextRun(scheduledInfo.Job.Definition.ID, nextRun); err != nil {
		return err
	}

	return nil
}

// estimateExecutionTime estimates the execution time for a plan
func (das *DependencyAwareScheduler) estimateExecutionTime(levels [][]string) time.Duration {
	// Simple estimation: assume each level takes 1 minute
	// In practice, this would use historical data
	return time.Duration(len(levels)) * time.Minute
}

// calculateNextRun calculates the next run time for a cron schedule
func calculateNextRun(schedule string, from time.Time) (time.Time, error) {
	// Simplified implementation - in production, use a proper cron parser
	return from.Add(time.Minute), nil
}

// SchedulerStats contains statistics about the dependency-aware scheduler
type SchedulerStats struct {
	TotalJobs       int             `json:"total_jobs"`
	PendingJobs     int             `json:"pending_jobs"`
	ReadyJobs       int             `json:"ready_jobs"`
	WaitingJobs     int             `json:"waiting_jobs"`
	RunningJobs     int             `json:"running_jobs"`
	CompletedJobs   int             `json:"completed_jobs"`
	FailedJobs      int             `json:"failed_jobs"`
	SkippedJobs     int             `json:"skipped_jobs"`
	DependencyStats DependencyStats `json:"dependency_stats"`
	LastUpdate      time.Time       `json:"last_update"`
}

// ValidateConfiguration validates the scheduler configuration
func (das *DependencyAwareScheduler) ValidateConfiguration() error {
	if das.config.MaxConcurrentJobs < 0 {
		return common.ErrValidationError("max_concurrent_jobs", fmt.Errorf("must be non-negative"))
	}

	if das.config.DependencyTimeout <= 0 {
		return common.ErrValidationError("dependency_timeout", fmt.Errorf("must be positive"))
	}

	if das.config.MaxRetries < 0 {
		return common.ErrValidationError("max_retries", fmt.Errorf("must be non-negative"))
	}

	if das.config.RetryBackoff <= 0 {
		return common.ErrValidationError("retry_backoff", fmt.Errorf("must be positive"))
	}

	validDeadlineHandling := []string{"skip", "delay", "force"}
	found := false
	for _, valid := range validDeadlineHandling {
		if das.config.DeadlineHandling == valid {
			found = true
			break
		}
	}
	if !found {
		return common.ErrValidationError("deadline_handling", fmt.Errorf("must be one of: %v", validDeadlineHandling))
	}

	return nil
}

// HealthCheck performs a health check on the scheduler
func (das *DependencyAwareScheduler) HealthCheck(ctx context.Context) error {
	das.mu.RLock()
	defer das.mu.RUnlock()

	// Check dependency resolver health
	if err := das.resolver.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("dependency_aware_scheduler", err)
	}

	// Check for too many failed jobs
	stats := das.GetSchedulerStats()
	if stats.FailedJobs > stats.TotalJobs/4 { // 25% failure rate threshold
		return common.ErrHealthCheckFailed("dependency_aware_scheduler", fmt.Errorf("too many failed jobs: %d", stats.FailedJobs))
	}

	return nil
}

// DefaultSchedulerConfig returns default scheduler configuration
func DefaultSchedulerConfig() *SchedulerConfig {
	return &SchedulerConfig{
		MaxConcurrentJobs:    10,
		DependencyTimeout:    30 * time.Minute,
		RetryOnFailure:       true,
		MaxRetries:           3,
		RetryBackoff:         5 * time.Second,
		SkipFailedDeps:       false,
		ParallelExecution:    true,
		DeadlineHandling:     "delay",
		PriorityScheduling:   true,
		OptimisticScheduling: false,
	}
}
