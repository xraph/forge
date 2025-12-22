package cron

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/cron/core"
)

func init() {
	// Register built-in scheduler factories to avoid import cycles.
	// These are registered here because the scheduler subpackage imports cron,
	// which would create a cycle if we tried to import scheduler from extension.go.

	// Register simple scheduler factory
	core.RegisterSchedulerFactory("simple", func(configInterface interface{}, deps *core.SchedulerDeps) (core.Scheduler, error) {
		config, ok := configInterface.(Config)
		if !ok {
			return nil, fmt.Errorf("invalid config type for simple scheduler")
		}

		storage, ok := deps.Storage.(Storage)
		if !ok {
			return nil, fmt.Errorf("invalid storage type")
		}

		executor, ok := deps.Executor.(*Executor)
		if !ok {
			return nil, fmt.Errorf("invalid executor type")
		}

		registry, ok := deps.Registry.(*JobRegistry)
		if !ok {
			return nil, fmt.Errorf("invalid registry type")
		}

		logger, ok := deps.Logger.(forge.Logger)
		if !ok {
			return nil, fmt.Errorf("invalid logger type")
		}

		return newDefaultSimpleScheduler(config, storage, executor, registry, logger)
	})
}

// defaultSimpleScheduler implements a single-node scheduler using robfig/cron.
type defaultSimpleScheduler struct {
	config   Config
	storage  Storage
	executor *Executor
	registry *JobRegistry
	logger   forge.Logger

	cron       *cron.Cron
	jobs       map[string]*Job
	entryIDs   map[string]cron.EntryID
	jobsMutex  sync.RWMutex
	running    bool
	runningMux sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// newDefaultSimpleScheduler creates a new simple scheduler.
func newDefaultSimpleScheduler(
	config Config,
	storage Storage,
	executor *Executor,
	registry *JobRegistry,
	logger forge.Logger,
) (*defaultSimpleScheduler, error) {
	location, err := time.LoadLocation(config.DefaultTimezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone '%s': %w", config.DefaultTimezone, err)
	}

	cronOptions := []cron.Option{
		cron.WithLocation(location),
		cron.WithSeconds(),
		cron.WithLogger(defaultCronLogger{logger: logger}),
	}

	c := cron.New(cronOptions...)
	ctx, cancel := context.WithCancel(context.Background())

	return &defaultSimpleScheduler{
		config:   config,
		storage:  storage,
		executor: executor,
		registry: registry,
		logger:   logger,
		cron:     c,
		jobs:     make(map[string]*Job),
		entryIDs: make(map[string]cron.EntryID),
		running:  false,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start starts the scheduler and loads jobs from storage.
func (s *defaultSimpleScheduler) Start(ctx context.Context) error {
	s.runningMux.Lock()
	if s.running {
		s.runningMux.Unlock()
		return fmt.Errorf("scheduler already running")
	}
	s.running = true
	s.runningMux.Unlock()

	s.logger.Info("starting simple scheduler")

	jobs, err := s.storage.ListJobs(ctx)
	if err != nil {
		return fmt.Errorf("failed to load jobs: %w", err)
	}

	for _, jobInterface := range jobs {
		job, ok := jobInterface.(*Job)
		if !ok || job == nil {
			continue
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

	s.cron.Start()

	s.logger.Info("simple scheduler started",
		forge.F("jobs_loaded", len(s.jobs)),
	)

	return nil
}

// Stop stops the scheduler gracefully.
func (s *defaultSimpleScheduler) Stop(ctx context.Context) error {
	s.runningMux.Lock()
	if !s.running {
		s.runningMux.Unlock()
		return nil
	}
	s.running = false
	s.runningMux.Unlock()

	s.logger.Info("stopping simple scheduler")

	cronCtx := s.cron.Stop()

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
func (s *defaultSimpleScheduler) AddJob(jobInterface interface{}) error {
	job, ok := jobInterface.(*Job)
	if !ok || job == nil {
		return fmt.Errorf("invalid job type")
	}

	if !job.Enabled {
		return fmt.Errorf("job is disabled")
	}

	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()

	if _, exists := s.jobs[job.ID]; exists {
		return ErrJobAlreadyExists
	}

	if job.Schedule == "" {
		return fmt.Errorf("job schedule is empty")
	}

	location := job.Timezone
	if location == nil {
		loc, err := time.LoadLocation(s.config.DefaultTimezone)
		if err != nil {
			return fmt.Errorf("invalid timezone: %w", err)
		}
		location = loc
	}

	if job.HandlerName != "" && job.Handler == nil {
		handler, err := s.registry.Get(job.HandlerName)
		if err != nil {
			return fmt.Errorf("handler not found: %w", err)
		}
		job.Handler = handler
	}

	jobWrapper := s.createJobWrapper(job)

	entryID, err := s.cron.AddFunc(job.Schedule, jobWrapper)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}

	s.jobs[job.ID] = job
	s.entryIDs[job.ID] = entryID

	entry := s.cron.Entry(entryID)
	nextRun := entry.Next
	job.NextExecutionAt = &nextRun

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
func (s *defaultSimpleScheduler) RemoveJob(jobID string) error {
	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()

	entryID, exists := s.entryIDs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	s.cron.Remove(entryID)
	delete(s.jobs, jobID)
	delete(s.entryIDs, jobID)

	s.logger.Info("job removed from scheduler",
		forge.F("job_id", jobID),
	)

	return nil
}

// UpdateJob updates a job in the scheduler.
func (s *defaultSimpleScheduler) UpdateJob(jobInterface interface{}) error {
	job, ok := jobInterface.(*Job)
	if !ok || job == nil {
		return fmt.Errorf("invalid job type")
	}

	if err := s.RemoveJob(job.ID); err != nil && err != ErrJobNotFound {
		return err
	}

	if job.Enabled {
		return s.AddJob(job)
	}

	return nil
}

// TriggerJob manually triggers a job execution.
func (s *defaultSimpleScheduler) TriggerJob(ctx context.Context, jobID string) (string, error) {
	s.jobsMutex.RLock()
	job, exists := s.jobs[jobID]
	s.jobsMutex.RUnlock()

	if !exists {
		return "", ErrJobNotFound
	}

	s.logger.Info("manually triggering job",
		forge.F("job_id", jobID),
		forge.F("job_name", job.Name),
	)

	executionID, err := s.executor.Execute(ctx, job)
	if err != nil {
		return "", err
	}

	return executionID, nil
}

// GetJob retrieves a job by ID.
func (s *defaultSimpleScheduler) GetJob(jobID string) (interface{}, error) {
	s.jobsMutex.RLock()
	defer s.jobsMutex.RUnlock()

	job, exists := s.jobs[jobID]
	if !exists {
		return nil, ErrJobNotFound
	}

	return job, nil
}

// ListJobs lists all jobs.
func (s *defaultSimpleScheduler) ListJobs() ([]interface{}, error) {
	s.jobsMutex.RLock()
	defer s.jobsMutex.RUnlock()

	result := make([]interface{}, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, job)
	}
	return result, nil
}

// IsRunning returns whether the scheduler is running.
func (s *defaultSimpleScheduler) IsRunning() bool {
	s.runningMux.RLock()
	defer s.runningMux.RUnlock()
	return s.running
}

// IsLeader always returns true for simple scheduler (no distributed coordination).
func (s *defaultSimpleScheduler) IsLeader() bool {
	return true
}

// createJobWrapper creates a wrapper function for cron execution.
func (s *defaultSimpleScheduler) createJobWrapper(job *Job) func() {
	return func() {
		s.logger.Debug("cron trigger",
			forge.F("job_id", job.ID),
			forge.F("job_name", job.Name),
		)

		s.updateExecutionTimes(job)

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
func (s *defaultSimpleScheduler) updateExecutionTimes(job *Job) {
	s.jobsMutex.Lock()
	defer s.jobsMutex.Unlock()

	now := time.Now()
	job.LastExecutionAt = &now

	if entryID, exists := s.entryIDs[job.ID]; exists {
		entry := s.cron.Entry(entryID)
		nextRun := entry.Next
		job.NextExecutionAt = &nextRun

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

// defaultCronLogger adapts forge.Logger to cron.Logger interface.
type defaultCronLogger struct {
	logger forge.Logger
}

func (l defaultCronLogger) Info(msg string, keysAndValues ...interface{}) {
	fields := defaultConvertToFields(keysAndValues)
	l.logger.Info("cron: "+msg, fields...)
}

func (l defaultCronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	fields := make([]forge.Field, 0, len(keysAndValues)/2+1)
	fields = append(fields, forge.Error(err))
	fields = append(fields, defaultConvertToFields(keysAndValues)...)
	l.logger.Error("cron: "+msg, fields...)
}

func defaultConvertToFields(keysAndValues []interface{}) []forge.Field {
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
