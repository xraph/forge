package cron

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Scheduler manages job scheduling and execution timing
type Scheduler struct {
	// Configuration
	checkInterval time.Duration

	// Scheduled jobs
	jobs     map[string]*ScheduledJob
	jobQueue []*ScheduledJob
	mu       sync.RWMutex

	// State management
	running     bool
	paused      bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// ScheduledJob represents a job in the scheduler
type ScheduledJob struct {
	Job       *Job
	NextRun   time.Time
	LastRun   time.Time
	Scheduled bool
	Priority  int
	Attempts  int
	LastError error
	AddedAt   time.Time
	UpdatedAt time.Time
}

// NewScheduler creates a new job scheduler
func NewScheduler(checkInterval time.Duration, logger common.Logger, metrics common.Metrics) *Scheduler {
	return &Scheduler{
		checkInterval: checkInterval,
		jobs:          make(map[string]*ScheduledJob),
		jobQueue:      make([]*ScheduledJob, 0),
		stopChannel:   make(chan struct{}),
		logger:        logger,
		metrics:       metrics,
	}
}

// Start starts the scheduler
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return common.ErrLifecycleError("start", fmt.Errorf("scheduler already running"))
	}

	s.running = true
	s.paused = false

	// Start background processing
	s.wg.Add(1)
	go s.processingLoop(ctx)

	if s.logger != nil {
		s.logger.Info("scheduler started", logger.Duration("check_interval", s.checkInterval))
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.cron.scheduler_started").Inc()
	}

	return nil
}

// Stop stops the scheduler
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return common.ErrLifecycleError("stop", fmt.Errorf("scheduler not running"))
	}

	close(s.stopChannel)
	s.running = false

	// Wait for processing to finish
	s.wg.Wait()

	if s.logger != nil {
		s.logger.Info("scheduler stopped")
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.cron.scheduler_stopped").Inc()
	}

	return nil
}

// Pause pauses the scheduler
func (s *Scheduler) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.paused = true

	if s.logger != nil {
		s.logger.Info("scheduler paused")
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.cron.scheduler_paused").Inc()
	}
}

// Resume resumes the scheduler
func (s *Scheduler) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.paused = false

	if s.logger != nil {
		s.logger.Info("scheduler resumed")
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.cron.scheduler_resumed").Inc()
	}
}

// ScheduleJob adds a job to the scheduler
func (s *Scheduler) ScheduleJob(job *Job) error {
	if job == nil {
		return common.ErrValidationError("job", fmt.Errorf("job cannot be nil"))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if job already scheduled
	if _, exists := s.jobs[job.Definition.ID]; exists {
		return common.ErrServiceAlreadyExists(job.Definition.ID)
	}

	// Calculate next run time
	nextRun, err := s.calculateNextRun(job.Definition.Schedule, time.Now())
	if err != nil {
		return common.ErrInvalidConfig("schedule", err)
	}

	// Create scheduled job
	scheduledJob := &ScheduledJob{
		Job:       job,
		NextRun:   nextRun,
		Scheduled: true,
		Priority:  job.Definition.Priority,
		AddedAt:   time.Now(),
		UpdatedAt: time.Now(),
	}

	// Add to jobs map
	s.jobs[job.Definition.ID] = scheduledJob

	// Add to queue
	s.addToQueue(scheduledJob)

	if s.logger != nil {
		s.logger.Info("job scheduled",
			logger.String("job_id", job.Definition.ID),
			logger.Time("next_run", nextRun),
			logger.Int("priority", job.Definition.Priority),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.cron.jobs_scheduled").Inc()
		s.metrics.Gauge("forge.cron.scheduled_jobs_count").Set(float64(len(s.jobs)))
	}

	return nil
}

// UnscheduleJob removes a job from the scheduler
func (s *Scheduler) UnscheduleJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scheduledJob, exists := s.jobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Remove from jobs map
	delete(s.jobs, jobID)

	// Remove from queue
	s.removeFromQueue(scheduledJob)

	if s.logger != nil {
		s.logger.Info("job unscheduled", logger.String("job_id", jobID))
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.cron.jobs_unscheduled").Inc()
		s.metrics.Gauge("forge.cron.scheduled_jobs_count").Set(float64(len(s.jobs)))
	}

	return nil
}

// RescheduleJob updates the schedule for a job
func (s *Scheduler) RescheduleJob(jobID string, newSchedule string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scheduledJob, exists := s.jobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Calculate new next run time
	nextRun, err := s.calculateNextRun(newSchedule, time.Now())
	if err != nil {
		return common.ErrInvalidConfig("schedule", err)
	}

	// Update scheduled job
	scheduledJob.Job.Definition.Schedule = newSchedule
	scheduledJob.NextRun = nextRun
	scheduledJob.UpdatedAt = time.Now()

	// Reorder queue
	s.removeFromQueue(scheduledJob)
	s.addToQueue(scheduledJob)

	if s.logger != nil {
		s.logger.Info("job rescheduled",
			logger.String("job_id", jobID),
			logger.String("new_schedule", newSchedule),
			logger.Time("next_run", nextRun),
		)
	}

	if s.metrics != nil {
		s.metrics.Counter("forge.cron.jobs_rescheduled").Inc()
	}

	return nil
}

// GetDueJobs returns jobs that are due for execution
func (s *Scheduler) GetDueJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.paused {
		return nil
	}

	now := time.Now()
	var dueJobs []*Job

	for _, scheduledJob := range s.jobQueue {
		if scheduledJob.NextRun.After(now) {
			break // Queue is sorted, so we can break early
		}

		if scheduledJob.Scheduled && scheduledJob.Job.IsRunnable() {
			dueJobs = append(dueJobs, scheduledJob.Job)
		}
	}

	return dueJobs
}

// GetScheduledJobs returns all scheduled jobs
func (s *Scheduler) GetScheduledJobs() []*ScheduledJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*ScheduledJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}

	return jobs
}

// GetJobSchedule returns the schedule for a specific job
func (s *Scheduler) GetJobSchedule(jobID string) (*ScheduledJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scheduledJob, exists := s.jobs[jobID]
	if !exists {
		return nil, common.ErrServiceNotFound(jobID)
	}

	return scheduledJob, nil
}

// UpdateJobNextRun updates the next run time for a job
func (s *Scheduler) UpdateJobNextRun(jobID string, nextRun time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scheduledJob, exists := s.jobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Update next run time
	scheduledJob.NextRun = nextRun
	scheduledJob.UpdatedAt = time.Now()

	// Reorder queue
	s.removeFromQueue(scheduledJob)
	s.addToQueue(scheduledJob)

	if s.logger != nil {
		s.logger.Debug("job next run updated",
			logger.String("job_id", jobID),
			logger.Time("next_run", nextRun),
		)
	}

	return nil
}

// MarkJobExecuted marks a job as executed
func (s *Scheduler) MarkJobExecuted(jobID string, executionTime time.Time, success bool, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	scheduledJob, exists := s.jobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Update execution info
	scheduledJob.LastRun = executionTime
	scheduledJob.UpdatedAt = time.Now()
	scheduledJob.LastError = err

	if success {
		scheduledJob.Attempts = 0

		// Calculate next run time
		nextRun, scheduleErr := s.calculateNextRun(scheduledJob.Job.Definition.Schedule, executionTime)
		if scheduleErr != nil {
			if s.logger != nil {
				s.logger.Error("failed to calculate next run time",
					logger.String("job_id", jobID),
					logger.Error(scheduleErr),
				)
			}
		} else {
			scheduledJob.NextRun = nextRun

			// Reorder queue
			s.removeFromQueue(scheduledJob)
			s.addToQueue(scheduledJob)
		}
	} else {
		scheduledJob.Attempts++

		// Calculate retry time if needed
		if scheduledJob.Attempts < scheduledJob.Job.Definition.MaxRetries {
			retryDelay := s.calculateRetryDelay(scheduledJob.Attempts)
			scheduledJob.NextRun = executionTime.Add(retryDelay)

			// Reorder queue
			s.removeFromQueue(scheduledJob)
			s.addToQueue(scheduledJob)
		} else {
			// Max retries reached, disable job
			scheduledJob.Scheduled = false
			scheduledJob.Job.Status = JobStatusFailed
			s.removeFromQueue(scheduledJob)
		}
	}

	if s.metrics != nil {
		if success {
			s.metrics.Counter("forge.cron.job_executions_success").Inc()
		} else {
			s.metrics.Counter("forge.cron.job_executions_failed").Inc()
		}
	}

	return nil
}

// HealthCheck performs a health check on the scheduler
func (s *Scheduler) HealthCheck(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.running {
		return common.ErrHealthCheckFailed("scheduler", fmt.Errorf("scheduler not running"))
	}

	// Check for stale jobs
	staleThreshold := time.Now().Add(-1 * time.Hour)
	staleJobs := 0

	for _, scheduledJob := range s.jobs {
		if scheduledJob.NextRun.Before(staleThreshold) && scheduledJob.Scheduled {
			staleJobs++
		}
	}

	if staleJobs > len(s.jobs)/2 { // More than 50% stale
		return common.ErrHealthCheckFailed("scheduler", fmt.Errorf("too many stale jobs: %d", staleJobs))
	}

	return nil
}

// GetStats returns scheduler statistics
func (s *Scheduler) GetStats() SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := SchedulerStats{
		Running:       s.running,
		Paused:        s.paused,
		TotalJobs:     len(s.jobs),
		CheckInterval: s.checkInterval,
		LastUpdate:    time.Now(),
	}

	// Count jobs by status
	now := time.Now()
	for _, scheduledJob := range s.jobs {
		if scheduledJob.Scheduled {
			stats.ActiveJobs++
		} else {
			stats.InactiveJobs++
		}

		if scheduledJob.NextRun.Before(now) {
			stats.OverdueJobs++
		}

		if scheduledJob.LastError != nil {
			stats.FailedJobs++
		}
	}

	return stats
}

// processingLoop runs the main processing loop
func (s *Scheduler) processingLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChannel:
			return
		case <-ticker.C:
			s.processSchedule()
		case <-ctx.Done():
			return
		}
	}
}

// processSchedule processes the job schedule
func (s *Scheduler) processSchedule() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.paused {
		return
	}

	// Clean up completed jobs and reorder queue
	s.cleanupQueue()

	// Update metrics
	if s.metrics != nil {
		s.metrics.Gauge("forge.cron.scheduled_jobs_count").Set(float64(len(s.jobs)))
		s.metrics.Gauge("forge.cron.queue_length").Set(float64(len(s.jobQueue)))
	}
}

// addToQueue adds a job to the sorted queue
func (s *Scheduler) addToQueue(job *ScheduledJob) {
	s.jobQueue = append(s.jobQueue, job)
	s.sortQueue()
}

// removeFromQueue removes a job from the queue
func (s *Scheduler) removeFromQueue(job *ScheduledJob) {
	for i, queuedJob := range s.jobQueue {
		if queuedJob == job {
			s.jobQueue = append(s.jobQueue[:i], s.jobQueue[i+1:]...)
			break
		}
	}
}

// sortQueue sorts the job queue by next run time and priority
func (s *Scheduler) sortQueue() {
	sort.Slice(s.jobQueue, func(i, j int) bool {
		jobA := s.jobQueue[i]
		jobB := s.jobQueue[j]

		// Sort by next run time first
		if !jobA.NextRun.Equal(jobB.NextRun) {
			return jobA.NextRun.Before(jobB.NextRun)
		}

		// Then by priority (higher priority first)
		return jobA.Priority > jobB.Priority
	})
}

// cleanupQueue removes inactive jobs from the queue
func (s *Scheduler) cleanupQueue() {
	var activeQueue []*ScheduledJob

	for _, job := range s.jobQueue {
		if job.Scheduled && s.jobs[job.Job.Definition.ID] != nil {
			activeQueue = append(activeQueue, job)
		}
	}

	s.jobQueue = activeQueue
}

// calculateNextRun calculates the next run time for a cron expression
func (s *Scheduler) calculateNextRun(schedule string, from time.Time) (time.Time, error) {
	// Simplified cron parsing - in production, use a proper cron library
	// For now, return a fixed interval based on the schedule
	return getNextRunTime(schedule, from)
}

// calculateRetryDelay calculates the retry delay for a failed job
func (s *Scheduler) calculateRetryDelay(attempt int) time.Duration {
	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, max 60s
	delay := time.Duration(1<<uint(attempt)) * time.Second
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}
	return delay
}

// SchedulerStats contains scheduler statistics
type SchedulerStats struct {
	Running       bool          `json:"running"`
	Paused        bool          `json:"paused"`
	TotalJobs     int           `json:"total_jobs"`
	ActiveJobs    int           `json:"active_jobs"`
	InactiveJobs  int           `json:"inactive_jobs"`
	OverdueJobs   int           `json:"overdue_jobs"`
	FailedJobs    int           `json:"failed_jobs"`
	CheckInterval time.Duration `json:"check_interval"`
	LastUpdate    time.Time     `json:"last_update"`
}
