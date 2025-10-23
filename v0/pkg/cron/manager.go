package cron

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/cron/distribution"
	"github.com/xraph/forge/pkg/cron/election"
	"github.com/xraph/forge/pkg/cron/execution"
	"github.com/xraph/forge/pkg/cron/persistence"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/events"
	"github.com/xraph/forge/pkg/logger"
)

// CronManager implements the core cron management interface
type CronManager struct {
	// Configuration
	config *CronConfig
	nodeID string

	// Core components
	store           persistence.Store
	scheduler       *Scheduler
	executor        *execution.Executor
	leaderElector   election.LeaderElection
	distributor     *distribution.JobDistributor
	handlerRegistry *HandlerRegistry

	// Framework integration
	logger        common.Logger
	metrics       common.Metrics
	eventBus      events.EventBus
	healthChecker common.HealthChecker

	// Internal state
	jobs        map[string]*Job
	mu          sync.RWMutex
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Leader state
	isLeader     bool
	leadershipMu sync.RWMutex
}

// CronConfig contains configuration for the cron manager
type CronConfig struct {
	NodeID               string                   `json:"node_id" yaml:"node_id"`
	ClusterID            string                   `json:"cluster_id" yaml:"cluster_id"`
	MaxConcurrentJobs    int                      `json:"max_concurrent_jobs" yaml:"max_concurrent_jobs"`
	JobCheckInterval     time.Duration            `json:"job_check_interval" yaml:"job_check_interval"`
	LeaderElectionConfig *election.Config         `json:"leader_election" yaml:"leader_election"`
	DistributionConfig   *distribution.Config     `json:"distribution" yaml:"distribution"`
	ExecutionConfig      *execution.Config        `json:"execution" yaml:"execution"`
	StoreConfig          *persistence.StoreConfig `json:"store" yaml:"store"`
	EnableMetrics        bool                     `json:"enable_metrics" yaml:"enable_metrics"`
	EnableHealthChecks   bool                     `json:"enable_health_checks" yaml:"enable_health_checks"`
}

// NewCronManager creates a new cron manager
func NewCronManager(db database.Connection, config *CronConfig, logger common.Logger, metrics common.Metrics, eventBus events.EventBus, healthChecker common.HealthChecker) (*CronManager, error) {
	if config == nil {
		return nil, common.ErrInvalidConfig("config", fmt.Errorf("config cannot be nil"))
	}

	if err := validateCronConfig(config); err != nil {
		return nil, err
	}

	// Create store
	store, err := persistence.NewDatabaseStore(db, config.StoreConfig, logger)
	if err != nil {
		return nil, common.ErrServiceStartFailed("cron-manager", err)
	}

	// Create handler registry
	handlerRegistry := NewHandlerRegistry()

	// Create scheduler
	scheduler := NewScheduler(config.JobCheckInterval, logger, metrics)

	// Create executor
	executor, err := execution.NewExecutor(config.ExecutionConfig, logger, metrics)
	if err != nil {
		return nil, common.ErrServiceStartFailed("cron-manager", err)
	}

	// Create leader elector
	leaderElector, err := election.NewLeaderElection(config.LeaderElectionConfig, logger, metrics)
	if err != nil {
		return nil, common.ErrServiceStartFailed("cron-manager", err)
	}

	// Create distributor
	distributor, err := distribution.NewJobDistributor(config.DistributionConfig, logger, metrics)
	if err != nil {
		return nil, common.ErrServiceStartFailed("cron-manager", err)
	}

	manager := &CronManager{
		config:          config,
		nodeID:          config.NodeID,
		store:           store,
		scheduler:       scheduler,
		executor:        executor,
		leaderElector:   leaderElector,
		distributor:     distributor,
		handlerRegistry: handlerRegistry,
		logger:          logger,
		metrics:         metrics,
		eventBus:        eventBus,
		healthChecker:   healthChecker,
		jobs:            make(map[string]*Job),
		stopChannel:     make(chan struct{}),
	}

	// Set up leadership callback
	leaderElector.Subscribe(manager.onLeadershipChange)

	return manager, nil
}

// Name returns the service name
func (cm *CronManager) Name() string {
	return "cron-manager"
}

// Dependencies returns the service dependencies
func (cm *CronManager) Dependencies() []string {
	return []string{"database-manager", "event-bus", "metrics-collector"}
}

// OnStart starts the cron manager
func (cm *CronManager) Start(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.started {
		return common.ErrLifecycleError("start", fmt.Errorf("cron manager already started"))
	}

	if cm.logger != nil {
		cm.logger.Info("starting cron manager", logger.String("node_id", cm.nodeID))
	}

	// Start store
	if err := cm.store.HealthCheck(ctx); err != nil {
		return common.ErrServiceStartFailed("cron-manager", err)
	}

	// Start executor
	if err := cm.executor.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("cron-manager", err)
	}

	// Start leader election
	if err := cm.leaderElector.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("cron-manager", err)
	}

	// Start scheduler
	if err := cm.scheduler.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("cron-manager", err)
	}

	// Load existing jobs
	if err := cm.loadJobs(ctx); err != nil {
		return common.ErrServiceStartFailed("cron-manager", err)
	}

	// Start background tasks
	cm.wg.Add(1)
	go cm.mainLoop(ctx)

	cm.started = true

	if cm.logger != nil {
		cm.logger.Info("cron manager started", logger.String("node_id", cm.nodeID))
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cron.manager_started").Inc()
	}

	return nil
}

// OnStop stops the cron manager
func (cm *CronManager) Stop(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("cron manager not started"))
	}

	if cm.logger != nil {
		cm.logger.Info("stopping cron manager", logger.String("node_id", cm.nodeID))
	}

	// Signal stop
	close(cm.stopChannel)

	// Wait for background tasks to finish
	cm.wg.Wait()

	// Collect shutdown errors
	var shutdownErrors []error

	// Stop components
	if err := cm.scheduler.Stop(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop scheduler: %w", err))
	}

	if err := cm.executor.Stop(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop executor: %w", err))
	}

	if err := cm.leaderElector.Stop(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop leader elector: %w", err))
	}

	if err := cm.store.Close(); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to close store: %w", err))
	}

	// Return combined shutdown errors if any occurred
	if len(shutdownErrors) > 0 {
		return fmt.Errorf("cron manager shutdown completed with errors: %v", shutdownErrors)
	}

	cm.started = false

	if cm.logger != nil {
		cm.logger.Info("cron manager stopped")
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cron.manager_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs health check
func (cm *CronManager) OnHealthCheck(ctx context.Context) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if !cm.started {
		return common.ErrHealthCheckFailed("cron-manager", fmt.Errorf("cron manager not started"))
	}

	// Check store health
	if err := cm.store.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("cron-manager", err)
	}

	// Check executor health
	if err := cm.executor.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("cron-manager", err)
	}

	// Check scheduler health
	if err := cm.scheduler.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("cron-manager", err)
	}

	return nil
}

// RegisterJob registers a new job
func (cm *CronManager) RegisterJob(jobDef JobDefinition) error {
	if err := jobDef.Validate(); err != nil {
		return err
	}

	// Check if handler exists
	if _, err := cm.handlerRegistry.Get(jobDef.ID); err != nil {
		return common.ErrValidationError("handler", fmt.Errorf("handler not found for job %s", jobDef.ID))
	}

	// Create job
	job, err := NewJob(&jobDef)
	if err != nil {
		return err
	}

	// Store job
	ctx := context.Background()
	if err := cm.store.CreateJob(ctx, job); err != nil {
		return common.ErrContainerError("create_job", err)
	}

	// Add to local registry
	cm.mu.Lock()
	cm.jobs[job.Definition.ID] = job
	cm.mu.Unlock()

	// Schedule job
	cm.scheduler.ScheduleJob(job)

	// Emit event
	cm.emitJobEvent(JobEventTypeCreated, job, nil)

	if cm.logger != nil {
		cm.logger.Info("job registered",
			logger.String("job_id", job.Definition.ID),
			logger.String("job_name", job.Definition.Name),
			logger.String("schedule", job.Definition.Schedule),
		)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cron.jobs_registered").Inc()
	}

	return nil
}

// UnregisterJob unregisters a job
func (cm *CronManager) UnregisterJob(jobID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	job, exists := cm.jobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Remove from scheduler
	cm.scheduler.UnscheduleJob(jobID)

	// Remove from store
	ctx := context.Background()
	if err := cm.store.DeleteJob(ctx, jobID); err != nil {
		return common.ErrContainerError("delete_job", err)
	}

	// Remove from local registry
	delete(cm.jobs, jobID)

	// Emit event
	cm.emitJobEvent(JobEventTypeDeleted, job, nil)

	if cm.logger != nil {
		cm.logger.Info("job unregistered", logger.String("job_id", jobID))
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cron.jobs_unregistered").Inc()
	}

	return nil
}

// GetJob retrieves a job by ID
func (cm *CronManager) GetJob(jobID string) (*Job, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	job, exists := cm.jobs[jobID]
	if !exists {
		return nil, common.ErrServiceNotFound(jobID)
	}

	return job, nil
}

// GetJobs retrieves all jobs
func (cm *CronManager) GetJobs() []Job {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	jobs := make([]Job, 0, len(cm.jobs))
	for _, job := range cm.jobs {
		jobs = append(jobs, *job)
	}

	return jobs
}

// GetJobHistory retrieves job execution history
func (cm *CronManager) GetJobHistory(jobID string, limit int) ([]JobExecution, error) {
	ctx := context.Background()
	executions, err := cm.store.GetExecutionsByJob(ctx, jobID, limit)
	if err != nil {
		return nil, common.ErrContainerError("get_job_history", err)
	}

	result := make([]JobExecution, len(executions))
	for i, exec := range executions {
		result[i] = *exec
	}

	return result, nil
}

// TriggerJob triggers a job execution manually
func (cm *CronManager) TriggerJob(ctx context.Context, jobID string) error {
	cm.mu.RLock()
	job, exists := cm.jobs[jobID]
	cm.mu.RUnlock()

	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	if !job.IsRunnable() {
		return common.ErrValidationError("job_status", fmt.Errorf("job %s is not runnable", jobID))
	}

	// Execute job
	execution := NewJobExecution(jobID, cm.nodeID, 1)
	if err := cm.executeJob(ctx, job, execution); err != nil {
		return common.ErrContainerError("trigger_job", err)
	}

	if cm.logger != nil {
		cm.logger.Info("job triggered manually",
			logger.String("job_id", jobID),
			logger.String("execution_id", execution.ID),
		)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cron.jobs_triggered").Inc()
	}

	return nil
}

// GetStats returns cron system statistics
func (cm *CronManager) GetStats() CronStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	stats := CronStats{
		TotalJobs:   int64(len(cm.jobs)),
		ActiveNodes: 1, // TODO: Get from cluster
		LeaderNode:  cm.getLeaderNode(),
		LastUpdate:  time.Now(),
	}

	// Count jobs by status
	for _, job := range cm.jobs {
		switch job.Status {
		case JobStatusActive:
			stats.ActiveJobs++
		case JobStatusRunning:
			stats.RunningJobs++
		case JobStatusFailed:
			stats.FailedJobs++
		case JobStatusDisabled:
			stats.DisabledJobs++
		}
	}

	// Get execution statistics from store
	ctx := context.Background()
	if execStats, err := cm.store.GetExecutionsStatsForPeriod(ctx, time.Now().Add(-24*time.Hour), time.Now()); err == nil {
		stats.TotalExecutions = execStats.TotalExecutions
		stats.SuccessfulExecutions = execStats.SuccessfulExecutions
		stats.FailedExecutions = execStats.FailedExecutions
		stats.AverageJobDuration = execStats.AverageExecutionTime
		if execStats.TotalExecutions > 0 {
			stats.SuccessRate = float64(execStats.SuccessfulExecutions) / float64(execStats.TotalExecutions)
			stats.FailureRate = float64(execStats.FailedExecutions) / float64(execStats.TotalExecutions)
		}
	}

	// Check cluster health
	stats.ClusterHealthy = cm.leaderElector.IsLeader() || cm.getLeaderNode() != ""

	return stats
}

// loadJobs loads jobs from the store
func (cm *CronManager) loadJobs(ctx context.Context) error {
	jobs, err := cm.store.GetJobs(ctx, &JobFilter{})
	if err != nil {
		return err
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, job := range jobs {
		cm.jobs[job.Definition.ID] = job
		if job.IsRunnable() {
			cm.scheduler.ScheduleJob(job)
		}
	}

	if cm.logger != nil {
		cm.logger.Info("loaded jobs from store", logger.Int("count", len(jobs)))
	}

	return nil
}

// mainLoop runs the main event loop
func (cm *CronManager) mainLoop(ctx context.Context) {
	defer cm.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cm.stopChannel:
			return
		case <-ticker.C:
			cm.processScheduledJobs(ctx)
		}
	}
}

// processScheduledJobs processes jobs that are due for execution
func (cm *CronManager) processScheduledJobs(ctx context.Context) {
	if !cm.isLeader {
		return // Only leader processes jobs
	}

	dueJobs := cm.scheduler.GetDueJobs()
	for _, job := range dueJobs {
		cm.wg.Add(1)
		go func(job *Job) {
			defer cm.wg.Done()
			cm.processJob(ctx, job)
		}(job)
	}
}

// processJob processes a single job
func (cm *CronManager) processJob(ctx context.Context, job *Job) {
	// Check if job is still runnable
	if !job.IsRunnable() {
		return
	}

	// Try to lock job
	if err := cm.store.LockJob(ctx, job.Definition.ID, cm.nodeID, 5*time.Minute); err != nil {
		if cm.logger != nil {
			cm.logger.Debug("failed to lock job", logger.String("job_id", job.Definition.ID), logger.Error(err))
		}
		return
	}

	defer func() {
		cm.store.UnlockJob(ctx, job.Definition.ID, cm.nodeID)
	}()

	// Select execution node
	nodeID, err := cm.distributor.SelectNode(ctx, job)
	if err != nil {
		if cm.logger != nil {
			cm.logger.Error("failed to select node for job", logger.String("job_id", job.Definition.ID), logger.Error(err))
		}
		return
	}

	// Create execution
	execution := NewJobExecution(job.Definition.ID, nodeID, 1)

	// Execute job
	if err := cm.executeJob(ctx, job, execution); err != nil {
		if cm.logger != nil {
			cm.logger.Error("failed to execute job", logger.String("job_id", job.Definition.ID), logger.Error(err))
		}
	}

	// Update next run time
	nextRun, err := getNextRunTime(job.Definition.Schedule, time.Now())
	if err == nil {
		job.NextRun = nextRun
		cm.store.UpdateJobNextRun(ctx, job.Definition.ID, nextRun)
	}
}

// executeJob executes a job
func (cm *CronManager) executeJob(ctx context.Context, job *Job, execution *JobExecution) error {
	// Get handler
	handler, err := cm.handlerRegistry.Get(job.Definition.ID)
	if err != nil {
		return err
	}

	// Execute through executor
	return cm.executor.Execute(ctx, job, execution, handler)
}

// onLeadershipChange handles leadership changes
func (cm *CronManager) onLeadershipChange(isLeader bool, leaderID string) {
	cm.leadershipMu.Lock()
	defer cm.leadershipMu.Unlock()

	wasLeader := cm.isLeader
	cm.isLeader = isLeader

	if cm.logger != nil {
		if isLeader {
			cm.logger.Info("became leader", logger.String("node_id", cm.nodeID))
		} else {
			cm.logger.Info("lost leadership", logger.String("node_id", cm.nodeID), logger.String("new_leader", leaderID))
		}
	}

	// Handle leadership transition
	if isLeader && !wasLeader {
		// Became leader - start job processing
		if cm.started {
			ctx := context.Background()
			cm.loadJobs(ctx)
		}
	} else if !isLeader && wasLeader {
		// Lost leadership - stop job processing
		cm.scheduler.Pause()
	}

	if cm.metrics != nil {
		if isLeader {
			cm.metrics.Counter("forge.cron.leadership_gained").Inc()
		} else {
			cm.metrics.Counter("forge.cron.leadership_lost").Inc()
		}
	}
}

// getLeaderNode returns the current leader node
func (cm *CronManager) getLeaderNode() string {
	if cm.isLeader {
		return cm.nodeID
	}
	return cm.leaderElector.GetLeader()
}

// emitJobEvent emits a job event
func (cm *CronManager) emitJobEvent(eventType JobEventType, job *Job, err error) {
	if cm.eventBus == nil {
		return
	}

	event := &JobEvent{
		Type:      eventType,
		JobID:     job.Definition.ID,
		NodeID:    cm.nodeID,
		Status:    job.Status,
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	if err != nil {
		event.Error = err.Error()
	}

	// Store event
	ctx := context.Background()
	cm.store.CreateEvent(ctx, event)

	// Emit to event bus
	cm.eventBus.Publish(ctx, &events.Event{
		Type:        string(eventType),
		AggregateID: job.Definition.ID,
		Data:        event,
		Timestamp:   time.Now(),
	})
}

// validateCronConfig validates cron configuration
func validateCronConfig(config *CronConfig) error {
	if config.NodeID == "" {
		return common.ErrValidationError("node_id", fmt.Errorf("node ID is required"))
	}

	if config.ClusterID == "" {
		return common.ErrValidationError("cluster_id", fmt.Errorf("cluster ID is required"))
	}

	if config.MaxConcurrentJobs <= 0 {
		return common.ErrValidationError("max_concurrent_jobs", fmt.Errorf("max concurrent jobs must be positive"))
	}

	if config.JobCheckInterval <= 0 {
		return common.ErrValidationError("job_check_interval", fmt.Errorf("job check interval must be positive"))
	}

	if config.StoreConfig != nil {
		if err := persistence.ValidateStoreConfig(config.StoreConfig); err != nil {
			return err
		}
	}

	return nil
}

// DefaultCronConfig returns default cron configuration
func DefaultCronConfig() *CronConfig {
	return &CronConfig{
		NodeID:               "node-1",
		ClusterID:            "cron-cluster",
		MaxConcurrentJobs:    10,
		JobCheckInterval:     time.Second,
		LeaderElectionConfig: election.DefaultConfig(),
		DistributionConfig:   distribution.DefaultConfig(),
		ExecutionConfig:      execution.DefaultConfig(),
		StoreConfig:          persistence.DefaultStoreConfig(),
		EnableMetrics:        true,
		EnableHealthChecks:   true,
	}
}
