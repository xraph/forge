package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/xraph/forge"
	cronext "github.com/xraph/forge/extensions/cron"
	"github.com/xraph/forge/extensions/cron/core"
)

func init() {
	// Register the simple scheduler factory
	core.RegisterSchedulerFactory("simple", func(configInterface any, deps *core.SchedulerDeps) (core.Scheduler, error) {
		config, ok := configInterface.(cronext.Config)
		if !ok {
			return nil, errors.New("invalid config type for simple scheduler")
		}

		// Type assert dependencies
		storage, ok := deps.Storage.(cronext.Storage)
		if !ok {
			return nil, errors.New("invalid storage type")
		}

		executor, ok := deps.Executor.(*cronext.Executor)
		if !ok {
			return nil, errors.New("invalid executor type")
		}

		registry, ok := deps.Registry.(*cronext.JobRegistry)
		if !ok {
			return nil, errors.New("invalid registry type")
		}

		logger, ok := deps.Logger.(forge.Logger)
		if !ok {
			return nil, errors.New("invalid logger type")
		}

		// NewSimpleScheduler returns (*SimpleScheduler, error)
		scheduler, err := NewSimpleScheduler(config, storage, executor, registry, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create simple scheduler: %w", err)
		}

		return scheduler, nil
	})
}

// SimpleScheduler implements a single-node scheduler using robfig/cron.
// This scheduler runs on a single instance without distributed coordination.
type SimpleScheduler struct {
	config   cronext.Config
	storage  cronext.Storage
	executor *cronext.Executor
	registry *cronext.JobRegistry
	logger   forge.Logger

	// Cron scheduler
	cron *cron.Cron

	// Job tracking
	jobs       map[string]*cronext.Job // jobID -> job
	entryIDs   map[string]cron.EntryID // jobID -> cron entry ID
	jobsMutex  sync.RWMutex
	running    bool
	runningMux sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSimpleScheduler creates a new simple scheduler.
func NewSimpleScheduler(
	config cronext.Config,
	storage cronext.Storage,
	executor *cronext.Executor,
	registry *cronext.JobRegistry,
	logger forge.Logger,
) (*SimpleScheduler, error) {
	// Parse default timezone
	location, err := time.LoadLocation(config.DefaultTimezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone '%s': %w", config.DefaultTimezone, err)
	}

	// Create cron scheduler with options
	cronOptions := []cron.Option{
		cron.WithLocation(location),
		cron.WithSeconds(), // Support seconds precision
		cron.WithLogger(cronLogger{logger: logger}),
	}

	c := cron.New(cronOptions...)

	ctx, cancel := context.WithCancel(context.Background())

	return &SimpleScheduler{
		config:   config,
		storage:  storage,
		executor: executor,
		registry: registry,
		logger:   logger,
		cron:     c,
		jobs:     make(map[string]*cronext.Job),
		entryIDs: make(map[string]cron.EntryID),
		running:  false,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start starts the scheduler and loads jobs from storage.
func (s *SimpleScheduler) Start(ctx context.Context) error {
	s.runningMux.Lock()

	if s.running {
		s.runningMux.Unlock()

		return errors.New("scheduler already running")
	}

	s.running = true
	s.runningMux.Unlock()

	s.logger.Info("starting simple scheduler")

	// Load jobs from storage
	jobs, err := s.storage.ListJobs(ctx)
	if err != nil {
		return fmt.Errorf("failed to load jobs: %w", err)
	}

	// Add enabled jobs to scheduler
	for _, jobInterface := range jobs {
		job := ToJob(jobInterface)
		if job == nil {
			continue // Skip invalid job types
		}

		if job.Enabled {
			if err := s.AddJob(job); err != nil {
				s.logger.Error("failed to add job to scheduler",
					forge.F("job_id", job.ID),
					forge.F("job_name", job.Name),
					forge.F("error", err),
				)
			}
		}
	}

	// Start the cron scheduler
	s.cron.Start()

	s.logger.Info("simple scheduler started",
		forge.F("jobs_loaded", len(s.jobs)),
	)

	return nil
}

// Stop stops the scheduler gracefully.
func (s *SimpleScheduler) Stop(ctx context.Context) error {
	s.runningMux.Lock()

	if !s.running {
		s.runningMux.Unlock()

		return nil
	}

	s.running = false
	s.runningMux.Unlock()

	s.logger.Info("stopping simple scheduler")

	// Stop accepting new jobs
	cronCtx := s.cron.Stop()

	// Wait for running jobs to complete or timeout
	select {
	case <-cronCtx.Done():
		s.logger.Info("simple scheduler stopped")
	case <-ctx.Done():
		s.logger.Warn("simple scheduler stop timeout")
	}

	s.cancel()

	return nil
}

// AddJob adds a job to the scheduler.
func (s *SimpleScheduler) AddJob(jobInterface any) error {
	job := ToJob(jobInterface)
	if job == nil {
		return errors.New("invalid job type")
	}

	if !job.Enabled {
		return errors.New("job is disabled")
	}

	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()

	// Check if job already exists
	if _, exists := s.jobs[job.ID]; exists {
		return cronext.ErrJobAlreadyExists
	}

	// Validate schedule
	if job.Schedule == "" {
		return errors.New("job schedule is empty")
	}

	// Determine timezone
	location := job.Timezone
	if location == nil {
		loc, err := time.LoadLocation(s.config.DefaultTimezone)
		if err != nil {
			return fmt.Errorf("invalid timezone: %w", err)
		}

		location = loc
	}

	// Attach handler if it's a named handler
	if job.HandlerName != "" && job.Handler == nil {
		handler, err := s.registry.Get(job.HandlerName)
		if err != nil {
			return fmt.Errorf("handler not found: %w", err)
		}

		job.Handler = handler
	}

	// Create cron job wrapper
	jobWrapper := s.createJobWrapper(job)

	// Parse and add to cron scheduler
	entryID, err := s.cron.AddFunc(job.Schedule, jobWrapper)
	if err != nil {
		return fmt.Errorf("%w: %w", cronext.ErrInvalidSchedule, err)
	}

	// Store job and entry ID
	s.jobs[job.ID] = job
	s.entryIDs[job.ID] = entryID

	// Calculate next execution time
	entry := s.cron.Entry(entryID)
	nextRun := entry.Next
	job.NextExecutionAt = &nextRun

	// Update job in storage
	if err := s.storage.SaveJob(s.ctx, job); err != nil {
		s.logger.Error("failed to update job in storage",
			forge.F("job_id", job.ID),
			forge.F("error", err),
		)
	}

	s.logger.Info("job added to scheduler",
		forge.F("job_id", job.ID),
		forge.F("job_name", job.Name),
		forge.F("schedule", job.Schedule),
		forge.F("next_run", nextRun),
	)

	return nil
}

// RemoveJob removes a job from the scheduler.
func (s *SimpleScheduler) RemoveJob(jobID string) error {
	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()

	entryID, exists := s.entryIDs[jobID]
	if !exists {
		return cronext.ErrJobNotFound
	}

	// Remove from cron scheduler
	s.cron.Remove(entryID)

	// Remove from maps
	delete(s.jobs, jobID)
	delete(s.entryIDs, jobID)

	s.logger.Info("job removed from scheduler",
		forge.F("job_id", jobID),
	)

	return nil
}

// UpdateJob updates a job in the scheduler.
func (s *SimpleScheduler) UpdateJob(jobInterface any) error {
	job := ToJob(jobInterface)
	if job == nil {
		return errors.New("invalid job type")
	}

	// Remove old job
	if err := s.RemoveJob(job.ID); err != nil && !errors.Is(err, cronext.ErrJobNotFound) {
		return err
	}

	// Add updated job if enabled
	if job.Enabled {
		return s.AddJob(job)
	}

	return nil
}

// TriggerJob manually triggers a job execution.
func (s *SimpleScheduler) TriggerJob(ctx context.Context, jobID string) (string, error) {
	s.jobsMutex.RLock()
	job, exists := s.jobs[jobID]
	s.jobsMutex.RUnlock()

	if !exists {
		return "", cronext.ErrJobNotFound
	}

	s.logger.Info("manually triggering job",
		forge.F("job_id", jobID),
		forge.F("job_name", job.Name),
	)

	// Execute the job
	executionID, err := s.executor.Execute(ctx, job)
	if err != nil {
		return "", err
	}

	return executionID, nil
}

// GetJob retrieves a job by ID.
func (s *SimpleScheduler) GetJob(jobID string) (any, error) {
	s.jobsMutex.RLock()
	defer s.jobsMutex.RUnlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return nil, core.ErrJobNotFound
	}

	return job, nil
}

// ListJobs lists all jobs.
func (s *SimpleScheduler) ListJobs() ([]any, error) {
	s.jobsMutex.RLock()
	defer s.jobsMutex.RUnlock()

	jobs := make([]*cronext.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}

	return FromJobs(jobs), nil
}

// IsRunning returns whether the scheduler is running.
func (s *SimpleScheduler) IsRunning() bool {
	s.runningMux.RLock()
	defer s.runningMux.RUnlock()

	return s.running
}

// IsLeader always returns true for simple scheduler (no distributed coordination).
func (s *SimpleScheduler) IsLeader() bool {
	return true
}

// createJobWrapper creates a wrapper function for cron execution.
func (s *SimpleScheduler) createJobWrapper(job *cronext.Job) func() {
	return func() {
		s.logger.Debug("cron trigger",
			forge.F("job_id", job.ID),
			forge.F("job_name", job.Name),
		)

		// Update last/next execution times
		s.updateExecutionTimes(job)

		// Execute the job
		executionID, err := s.executor.Execute(s.ctx, job)
		if err != nil {
			s.logger.Error("failed to execute job",
				forge.F("job_id", job.ID),
				forge.F("job_name", job.Name),
				forge.F("error", err),
			)

			return
		}

		s.logger.Debug("job execution started",
			forge.F("job_id", job.ID),
			forge.F("execution_id", executionID),
		)
	}
}

// updateExecutionTimes updates the last and next execution times for a job.
func (s *SimpleScheduler) updateExecutionTimes(job *cronext.Job) {
	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()

	now := time.Now()
	job.LastExecutionAt = &now

	// Get next execution time from cron
	if entryID, exists := s.entryIDs[job.ID]; exists {
		entry := s.cron.Entry(entryID)
		nextRun := entry.Next
		job.NextExecutionAt = &nextRun

		// Update in storage (best effort, don't block)
		go func() {
			if err := s.storage.SaveJob(context.Background(), job); err != nil {
				s.logger.Error("failed to update job execution times",
					forge.F("job_id", job.ID),
					forge.F("error", err),
				)
			}
		}()
	}
}

// cronLogger adapts forge.Logger to cron.Logger interface.
type cronLogger struct {
	logger forge.Logger
}

func (l cronLogger) Info(msg string, keysAndValues ...any) {
	// Convert keysAndValues to forge.Field format
	fields := convertToFields(keysAndValues)
	l.logger.Info("cron: "+msg, fields...)
}

func (l cronLogger) Error(err error, msg string, keysAndValues ...any) {
	fields := make([]forge.Field, 0, len(keysAndValues)/2+1)
	fields = append(fields, forge.Error(err))
	fields = append(fields, convertToFields(keysAndValues)...)
	l.logger.Error("cron: "+msg, fields...)
}

// convertToFields converts key-value pairs to forge.Field format.
func convertToFields(keysAndValues []any) []forge.Field {
	fields := make([]forge.Field, 0, len(keysAndValues)/2)
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			key, ok := keysAndValues[i].(string)
			if ok {
				fields = append(fields, forge.Any(key, keysAndValues[i+1]))
			}
		}
	}

	return fields
}
