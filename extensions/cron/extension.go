package cron

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/cron/core"
)

// Extension implements forge.Extension and forge.RunnableExtension for cron functionality.
type Extension struct {
	*forge.BaseExtension

	config    Config
	storage   Storage
	executor  *Executor
	scheduler Scheduler
	registry  *JobRegistry
	nodeID    string

	// Start time for uptime tracking
	startTime time.Time
}

// NewExtension creates a new cron extension with functional options.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("cron", "1.0.0", "Cron job scheduler with distributed support")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new cron extension with a complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the cron extension with the app.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load configuration
	programmaticConfig := e.config
	finalConfig := DefaultConfig()

	if err := e.LoadConfig("cron", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("cron: failed to load required config: %w", err)
		}

		e.Logger().Warn("cron: using default/programmatic config", forge.F("error", err.Error()))
	}

	e.config = finalConfig

	// Validate configuration
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("cron config validation failed: %w", err)
	}

	// Generate node ID if not provided
	if e.config.NodeID == "" {
		e.nodeID = uuid.New().String()
	} else {
		e.nodeID = e.config.NodeID
	}

	e.Logger().Info("registering cron extension",
		forge.F("mode", e.config.Mode),
		forge.F("storage", e.config.Storage),
		forge.F("node_id", e.nodeID),
	)

	// Initialize storage
	if err := e.initializeStorage(app); err != nil {
		return err
	}

	// Initialize job registry
	e.registry = NewJobRegistry()

	// Register job registry in DI
	if err := forge.RegisterSingleton(app.Container(), "cron.registry", func(c forge.Container) (*JobRegistry, error) {
		return e.registry, nil
	}); err != nil {
		return fmt.Errorf("failed to register job registry: %w", err)
	}

	// Initialize executor
	e.executor = NewExecutor(e.config, e.storage, e.registry, e.Logger(), e.Metrics(), e.nodeID)

	// Register executor in DI
	if err := forge.RegisterSingleton(app.Container(), "cron.executor", func(c forge.Container) (*Executor, error) {
		return e.executor, nil
	}); err != nil {
		return fmt.Errorf("failed to register executor: %w", err)
	}

	// Initialize scheduler
	if err := e.initializeScheduler(); err != nil {
		return err
	}

	// Register scheduler in DI
	if err := forge.RegisterSingleton(app.Container(), "cron.scheduler", func(c forge.Container) (Scheduler, error) {
		return e.scheduler, nil
	}); err != nil {
		return fmt.Errorf("failed to register scheduler: %w", err)
	}

	// Register storage in DI
	if err := forge.RegisterSingleton(app.Container(), "cron.storage", func(c forge.Container) (Storage, error) {
		return e.storage, nil
	}); err != nil {
		return fmt.Errorf("failed to register storage: %w", err)
	}

	e.Logger().Info("cron extension registered")

	return nil
}

// initializeStorage initializes the storage backend.
func (e *Extension) initializeStorage(app forge.App) error {
	// Create storage using the factory pattern
	storage, err := core.CreateStorage(e.config.Storage, e.config)
	if err != nil {
		return fmt.Errorf("failed to create %s storage: %w", e.config.Storage, err)
	}

	e.storage = storage

	return nil
}

// initializeScheduler initializes the scheduler based on mode.
func (e *Extension) initializeScheduler() error {
	// Prepare scheduler dependencies
	deps := &core.SchedulerDeps{
		Storage:  e.storage,
		Executor: e.executor,
		Registry: e.registry,
		Logger:   e.Logger(),
	}

	// Create scheduler using the factory pattern
	sched, err := core.CreateScheduler(e.config.Mode, e.config, deps)
	if err != nil {
		return fmt.Errorf("failed to create %s scheduler: %w", e.config.Mode, err)
	}

	e.scheduler = sched

	return nil
}

// Start starts the cron extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting cron extension")

	e.startTime = time.Now()

	// Connect to storage
	if err := e.storage.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to storage: %w", err)
	}

	// Start scheduler
	if err := e.scheduler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("cron extension started")

	return nil
}

// Run implements forge.RunnableExtension.
// This is called after the app starts to run any long-running processes.
func (e *Extension) Run(ctx context.Context) error {
	e.Logger().Info("cron extension running")

	// The scheduler is already running, just keep it alive
	// In the future, this could run background tasks like:
	// - History cleanup
	// - Metrics collection
	// - Health checks

	return nil
}

// Stop stops the cron extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping cron extension")

	// Stop scheduler
	if e.scheduler != nil {
		if err := e.scheduler.Stop(ctx); err != nil {
			e.Logger().Error("failed to stop scheduler", forge.F("error", err))
		}
	}

	// Disconnect from storage
	if e.storage != nil {
		if err := e.storage.Disconnect(ctx); err != nil {
			e.Logger().Error("failed to disconnect storage", forge.F("error", err))
		}
	}

	e.MarkStopped()
	e.Logger().Info("cron extension stopped")

	return nil
}

// Shutdown implements forge.RunnableExtension.
// This is called before Stop to gracefully shutdown long-running processes.
func (e *Extension) Shutdown(ctx context.Context) error {
	e.Logger().Info("shutting down cron extension")

	// Shutdown executor (wait for running jobs)
	if e.executor != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, e.config.ShutdownTimeout)
		defer cancel()

		if err := e.executor.Shutdown(shutdownCtx); err != nil {
			e.Logger().Error("executor shutdown failed", forge.F("error", err))

			return err
		}
	}

	return nil
}

// Health checks if the cron extension is healthy.
func (e *Extension) Health(ctx context.Context) error {
	// Check storage connectivity
	if err := e.storage.Ping(ctx); err != nil {
		return fmt.Errorf("storage health check failed: %w", err)
	}

	// Check scheduler status
	if !e.scheduler.IsRunning() {
		return ErrSchedulerNotRunning
	}

	return nil
}

// Dependencies returns the names of extensions this extension depends on.
func (e *Extension) Dependencies() []string {
	deps := []string{}

	// Add database dependency if using database storage
	if e.config.Storage == "database" {
		deps = append(deps, "database")
	}

	// Add consensus dependency if using distributed mode with leader election
	if e.config.Mode == "distributed" && e.config.LeaderElection {
		deps = append(deps, e.config.ConsensusExtension)
	}

	return deps
}

// GetScheduler returns the scheduler instance.
// This is useful for registering jobs programmatically.
func (e *Extension) GetScheduler() Scheduler {
	return e.scheduler
}

// GetRegistry returns the job registry.
// This is useful for registering job handlers.
func (e *Extension) GetRegistry() *JobRegistry {
	return e.registry
}

// GetExecutor returns the executor instance.
func (e *Extension) GetExecutor() *Executor {
	return e.executor
}

// GetStorage returns the storage instance.
func (e *Extension) GetStorage() Storage {
	return e.storage
}

// GetStats returns scheduler statistics.
func (e *Extension) GetStats(ctx context.Context) (*SchedulerStats, error) {
	jobs, err := e.scheduler.ListJobs()
	if err != nil {
		return nil, err
	}

	stats := &SchedulerStats{
		TotalJobs:         len(jobs),
		EnabledJobs:       0,
		DisabledJobs:      0,
		RunningExecutions: e.executor.GetRunningCount(),
		QueuedExecutions:  0,
		IsLeader:          e.scheduler.IsLeader(),
		NodeID:            e.nodeID,
		Uptime:            time.Since(e.startTime),
		JobStats:          make(map[string]int),
	}

	for _, jobInterface := range jobs {
		job, ok := jobInterface.(*Job)
		if !ok {
			continue // Skip invalid job types
		}

		if job.Enabled {
			stats.EnabledJobs++
		} else {
			stats.DisabledJobs++
		}
	}

	// Get execution counts by status
	filter := &ExecutionFilter{}
	for _, status := range []ExecutionStatus{
		ExecutionStatusPending,
		ExecutionStatusRunning,
		ExecutionStatusSuccess,
		ExecutionStatusFailed,
		ExecutionStatusCancelled,
		ExecutionStatusTimeout,
	} {
		filter.Status = []ExecutionStatus{status}

		count, err := e.storage.GetExecutionCount(ctx, filter)
		if err == nil {
			stats.JobStats[string(status)] = int(count)
		}
	}

	return stats, nil
}

// CreateJob creates a new job and adds it to the scheduler.
func (e *Extension) CreateJob(ctx context.Context, job *Job) error {
	// Set timestamps
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}

	job.UpdatedAt = now

	// Save to storage
	if err := e.storage.SaveJob(ctx, job); err != nil {
		return err
	}

	// Add to scheduler if enabled
	if job.Enabled {
		if err := e.scheduler.AddJob(job); err != nil {
			return err
		}
	}

	return nil
}

// UpdateJob updates an existing job.
func (e *Extension) UpdateJob(ctx context.Context, jobID string, update *JobUpdate) error {
	// Get existing job
	job, err := e.storage.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	// Update storage
	if err := e.storage.UpdateJob(ctx, jobID, update); err != nil {
		return err
	}

	// Get updated job
	updatedJob, err := e.storage.GetJob(ctx, jobID)
	if err != nil {
		return err
	}

	// Update scheduler
	if err := e.scheduler.UpdateJob(updatedJob); err != nil {
		// If scheduler update fails, revert storage (best effort)
		e.storage.SaveJob(ctx, job)

		return err
	}

	return nil
}

// DeleteJob deletes a job.
func (e *Extension) DeleteJob(ctx context.Context, jobID string) error {
	// Remove from scheduler
	if err := e.scheduler.RemoveJob(jobID); err != nil && !errors.Is(err, ErrJobNotFound) {
		return err
	}

	// Delete from storage
	if err := e.storage.DeleteJob(ctx, jobID); err != nil {
		return err
	}

	// Optionally delete execution history
	if _, err := e.storage.DeleteExecutionsByJob(ctx, jobID); err != nil {
		e.Logger().Error("failed to delete job executions",
			forge.F("job_id", jobID),
			forge.F("error", err),
		)
	}

	return nil
}

// TriggerJob manually triggers a job execution.
func (e *Extension) TriggerJob(ctx context.Context, jobID string) (string, error) {
	return e.scheduler.TriggerJob(ctx, jobID)
}
