package dependencies

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	cron "github.com/xraph/forge/v0/pkg/cron/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// DependencyResolver manages job dependencies and resolves execution order
type DependencyResolver struct {
	// Dependency graph
	graph *DependencyGraph

	// Job registry
	jobs map[string]*cron.Job

	// Execution tracking
	executions map[string]*ExecutionState

	// Concurrency control
	mu sync.RWMutex

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// ExecutionState tracks the state of job execution for dependency resolution
type ExecutionState struct {
	JobID         string
	Status        ExecutionStatus
	StartTime     time.Time
	EndTime       time.Time
	Dependencies  []string
	Dependents    []string
	WaitingFor    []string
	Notifications chan DependencyEvent
	mu            sync.RWMutex
}

// ExecutionStatus represents the execution status for dependency tracking
type ExecutionStatus string

const (
	ExecutionStatusWaiting   ExecutionStatus = "waiting"
	ExecutionStatusReady     ExecutionStatus = "ready"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
	ExecutionStatusCancelled ExecutionStatus = "cancelled"
	ExecutionStatusSkipped   ExecutionStatus = "skipped"
)

// DependencyEvent represents a dependency-related event
type DependencyEvent struct {
	Type      DependencyEventType
	JobID     string
	Status    ExecutionStatus
	Timestamp time.Time
	Error     error
}

// DependencyEventType represents the type of dependency event
type DependencyEventType string

const (
	DependencyEventTypeCompleted DependencyEventType = "completed"
	DependencyEventTypeFailed    DependencyEventType = "failed"
	DependencyEventTypeSkipped   DependencyEventType = "skipped"
	DependencyEventTypeCancelled DependencyEventType = "cancelled"
)

// DependencyResolution represents the result of dependency resolution
type DependencyResolution struct {
	JobID        string
	CanExecute   bool
	Reason       string
	Dependencies []string
	MissingDeps  []string
	FailedDeps   []string
	WaitTime     time.Duration
}

// NewDependencyResolver creates a new dependency resolver
func NewDependencyResolver(logger common.Logger, metrics common.Metrics) *DependencyResolver {
	return &DependencyResolver{
		graph:      NewDependencyGraph(),
		jobs:       make(map[string]*cron.Job),
		executions: make(map[string]*ExecutionState),
		logger:     logger,
		metrics:    metrics,
	}
}

// RegisterJob registers a job and its dependencies
func (dr *DependencyResolver) RegisterJob(job *cron.Job) error {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	if job == nil {
		return common.ErrValidationError("job", fmt.Errorf("job cannot be nil"))
	}

	jobID := job.Definition.ID
	dr.jobs[jobID] = job

	// Add job to dependency graph
	if err := dr.graph.AddJob(jobID, job.Definition.Dependencies); err != nil {
		return err
	}

	if dr.logger != nil {
		dr.logger.Info("job registered in dependency resolver",
			logger.String("job_id", jobID),
			logger.String("dependencies", fmt.Sprintf("%v", job.Definition.Dependencies)),
		)
	}

	if dr.metrics != nil {
		dr.metrics.Counter("forge.cron.dependencies.jobs_registered").Inc()
	}

	return nil
}

// UnregisterJob removes a job from the dependency resolver
func (dr *DependencyResolver) UnregisterJob(jobID string) error {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	if _, exists := dr.jobs[jobID]; !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Remove from dependency graph
	if err := dr.graph.RemoveJob(jobID); err != nil {
		return err
	}

	// Clean up execution state
	delete(dr.executions, jobID)
	delete(dr.jobs, jobID)

	if dr.logger != nil {
		dr.logger.Info("job unregistered from dependency resolver", logger.String("job_id", jobID))
	}

	if dr.metrics != nil {
		dr.metrics.Counter("forge.cron.dependencies.jobs_unregistered").Inc()
	}

	return nil
}

// ResolveJobDependencies resolves whether a job can be executed based on its dependencies
func (dr *DependencyResolver) ResolveJobDependencies(ctx context.Context, jobID string) (*DependencyResolution, error) {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	job, exists := dr.jobs[jobID]
	if !exists {
		return nil, common.ErrServiceNotFound(jobID)
	}

	resolution := &DependencyResolution{
		JobID:        jobID,
		CanExecute:   true,
		Dependencies: job.Definition.Dependencies,
		MissingDeps:  make([]string, 0),
		FailedDeps:   make([]string, 0),
	}

	// Check if job has dependencies
	if len(job.Definition.Dependencies) == 0 {
		resolution.Reason = "no dependencies"
		return resolution, nil
	}

	// Check each dependency
	for _, depID := range job.Definition.Dependencies {
		depState, exists := dr.executions[depID]
		if !exists {
			resolution.MissingDeps = append(resolution.MissingDeps, depID)
			resolution.CanExecute = false
			continue
		}

		depState.mu.RLock()
		status := depState.Status
		depState.mu.RUnlock()

		switch status {
		case ExecutionStatusCompleted:
			// Dependency satisfied
			continue
		case ExecutionStatusFailed:
			resolution.FailedDeps = append(resolution.FailedDeps, depID)
			resolution.CanExecute = false
		case ExecutionStatusCancelled:
			resolution.FailedDeps = append(resolution.FailedDeps, depID)
			resolution.CanExecute = false
		default:
			// Dependency not yet completed
			resolution.MissingDeps = append(resolution.MissingDeps, depID)
			resolution.CanExecute = false
		}
	}

	// Set resolution reason
	if !resolution.CanExecute {
		if len(resolution.FailedDeps) > 0 {
			resolution.Reason = fmt.Sprintf("dependencies failed: %v", resolution.FailedDeps)
		} else {
			resolution.Reason = fmt.Sprintf("waiting for dependencies: %v", resolution.MissingDeps)
		}
	} else {
		resolution.Reason = "all dependencies satisfied"
	}

	if dr.metrics != nil {
		if resolution.CanExecute {
			dr.metrics.Counter("forge.cron.dependencies.resolutions_ready").Inc()
		} else {
			dr.metrics.Counter("forge.cron.dependencies.resolutions_waiting").Inc()
		}
	}

	return resolution, nil
}

// StartJobExecution marks a job as starting execution
func (dr *DependencyResolver) StartJobExecution(ctx context.Context, jobID string) error {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	job, exists := dr.jobs[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Create or update execution state
	execState := &ExecutionState{
		JobID:         jobID,
		Status:        ExecutionStatusRunning,
		StartTime:     time.Now(),
		Dependencies:  job.Definition.Dependencies,
		Dependents:    dr.graph.GetDependents(jobID),
		Notifications: make(chan DependencyEvent, 100),
	}

	dr.executions[jobID] = execState

	if dr.logger != nil {
		dr.logger.Info("job execution started",
			logger.String("job_id", jobID),
			logger.String("dependencies", fmt.Sprintf("%v", execState.Dependencies)),
		)
	}

	if dr.metrics != nil {
		dr.metrics.Counter("forge.cron.dependencies.executions_started").Inc()
	}

	return nil
}

// CompleteJobExecution marks a job as completed and notifies dependents
func (dr *DependencyResolver) CompleteJobExecution(ctx context.Context, jobID string, success bool, err error) error {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	execState, exists := dr.executions[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	execState.mu.Lock()
	execState.EndTime = time.Now()
	if success {
		execState.Status = ExecutionStatusCompleted
	} else {
		execState.Status = ExecutionStatusFailed
	}
	execState.mu.Unlock()

	// Notify dependents
	eventType := DependencyEventTypeCompleted
	if !success {
		eventType = DependencyEventTypeFailed
	}

	event := DependencyEvent{
		Type:      eventType,
		JobID:     jobID,
		Status:    execState.Status,
		Timestamp: time.Now(),
		Error:     err,
	}

	dr.notifyDependents(jobID, event)

	if dr.logger != nil {
		dr.logger.Info("job execution completed",
			logger.String("job_id", jobID),
			logger.Bool("success", success),
			logger.String("duration", execState.EndTime.Sub(execState.StartTime).String()),
		)
	}

	if dr.metrics != nil {
		if success {
			dr.metrics.Counter("forge.cron.dependencies.executions_completed").Inc()
		} else {
			dr.metrics.Counter("forge.cron.dependencies.executions_failed").Inc()
		}
		dr.metrics.Histogram("forge.cron.dependencies.execution_duration").Observe(execState.EndTime.Sub(execState.StartTime).Seconds())
	}

	return nil
}

// CancelJobExecution marks a job as cancelled
func (dr *DependencyResolver) CancelJobExecution(ctx context.Context, jobID string) error {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	execState, exists := dr.executions[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	execState.mu.Lock()
	execState.Status = ExecutionStatusCancelled
	execState.EndTime = time.Now()
	execState.mu.Unlock()

	// Notify dependents
	event := DependencyEvent{
		Type:      DependencyEventTypeCancelled,
		JobID:     jobID,
		Status:    ExecutionStatusCancelled,
		Timestamp: time.Now(),
	}

	dr.notifyDependents(jobID, event)

	if dr.logger != nil {
		dr.logger.Info("job execution cancelled", logger.String("job_id", jobID))
	}

	if dr.metrics != nil {
		dr.metrics.Counter("forge.cron.dependencies.executions_cancelled").Inc()
	}

	return nil
}

// GetReadyJobs returns jobs that are ready to execute (all dependencies satisfied)
func (dr *DependencyResolver) GetReadyJobs(ctx context.Context) ([]string, error) {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	var readyJobs []string

	for jobID := range dr.jobs {
		resolution, err := dr.ResolveJobDependencies(ctx, jobID)
		if err != nil {
			continue
		}

		if resolution.CanExecute {
			readyJobs = append(readyJobs, jobID)
		}
	}

	return readyJobs, nil
}

// GetJobExecutionState returns the execution state of a job
func (dr *DependencyResolver) GetJobExecutionState(jobID string) (*ExecutionState, error) {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	execState, exists := dr.executions[jobID]
	if !exists {
		return nil, common.ErrServiceNotFound(jobID)
	}

	// Return a copy to avoid race conditions
	execState.mu.RLock()
	defer execState.mu.RUnlock()

	return &ExecutionState{
		JobID:        execState.JobID,
		Status:       execState.Status,
		StartTime:    execState.StartTime,
		EndTime:      execState.EndTime,
		Dependencies: execState.Dependencies,
		Dependents:   execState.Dependents,
		WaitingFor:   execState.WaitingFor,
	}, nil
}

// GetDependencyGraph returns the dependency graph
func (dr *DependencyResolver) GetDependencyGraph() *DependencyGraph {
	return dr.graph
}

// ValidateJobDependencies validates that all job dependencies are valid
func (dr *DependencyResolver) ValidateJobDependencies() error {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	// Check for circular dependencies
	if err := dr.graph.ValidateNoCycles(); err != nil {
		return err
	}

	// Check that all dependencies exist
	for jobID, job := range dr.jobs {
		for _, depID := range job.Definition.Dependencies {
			if _, exists := dr.jobs[depID]; !exists {
				return common.ErrDependencyNotFound(jobID, depID)
			}
		}
	}

	return nil
}

// GetJobDependencies returns the dependencies of a job
func (dr *DependencyResolver) GetJobDependencies(jobID string) ([]string, error) {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	job, exists := dr.jobs[jobID]
	if !exists {
		return nil, common.ErrServiceNotFound(jobID)
	}

	return job.Definition.Dependencies, nil
}

// GetJobDependents returns the dependents of a job
func (dr *DependencyResolver) GetJobDependents(jobID string) []string {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	return dr.graph.GetDependents(jobID)
}

// WaitForDependencies waits for the dependencies of a job to complete
func (dr *DependencyResolver) WaitForDependencies(ctx context.Context, jobID string, timeout time.Duration) error {
	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return common.ErrTimeoutError("wait_for_dependencies", timeout)
		case <-ticker.C:
			resolution, err := dr.ResolveJobDependencies(ctx, jobID)
			if err != nil {
				return err
			}

			if resolution.CanExecute {
				return nil
			}

			// Check if any dependencies have failed
			if len(resolution.FailedDeps) > 0 {
				return common.ErrDependencyNotFound(jobID, fmt.Sprintf("failed dependencies: %v", resolution.FailedDeps))
			}
		}
	}
}

// notifyDependents notifies all dependents of a job about its completion
func (dr *DependencyResolver) notifyDependents(jobID string, event DependencyEvent) {
	dependents := dr.graph.GetDependents(jobID)

	for _, depID := range dependents {
		if execState, exists := dr.executions[depID]; exists {
			select {
			case execState.Notifications <- event:
			default:
				// Channel is full, skip notification
				if dr.logger != nil {
					dr.logger.Warn("failed to notify dependent job",
						logger.String("job_id", jobID),
						logger.String("dependent_id", depID),
					)
				}
			}
		}
	}
}

// GetDependencyStats returns statistics about dependency resolution
func (dr *DependencyResolver) GetDependencyStats() DependencyStats {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	stats := DependencyStats{
		TotalJobs:       len(dr.jobs),
		TotalExecutions: len(dr.executions),
		LastUpdate:      time.Now(),
	}

	// Count executions by status
	for _, execState := range dr.executions {
		execState.mu.RLock()
		switch execState.Status {
		case ExecutionStatusWaiting:
			stats.WaitingJobs++
		case ExecutionStatusReady:
			stats.ReadyJobs++
		case ExecutionStatusRunning:
			stats.RunningJobs++
		case ExecutionStatusCompleted:
			stats.CompletedJobs++
		case ExecutionStatusFailed:
			stats.FailedJobs++
		case ExecutionStatusCancelled:
			stats.CancelledJobs++
		}
		execState.mu.RUnlock()
	}

	// Calculate dependency metrics
	stats.TotalDependencies = dr.graph.GetTotalDependencies()
	stats.MaxDependencyChainLength = dr.graph.GetMaxChainLength()
	stats.CircularDependencies = dr.graph.HasCycles()

	return stats
}

// DependencyStats contains statistics about dependency resolution
type DependencyStats struct {
	TotalJobs                int       `json:"total_jobs"`
	TotalExecutions          int       `json:"total_executions"`
	WaitingJobs              int       `json:"waiting_jobs"`
	ReadyJobs                int       `json:"ready_jobs"`
	RunningJobs              int       `json:"running_jobs"`
	CompletedJobs            int       `json:"completed_jobs"`
	FailedJobs               int       `json:"failed_jobs"`
	CancelledJobs            int       `json:"cancelled_jobs"`
	TotalDependencies        int       `json:"total_dependencies"`
	MaxDependencyChainLength int       `json:"max_dependency_chain_length"`
	CircularDependencies     bool      `json:"circular_dependencies"`
	LastUpdate               time.Time `json:"last_update"`
}

// ResetExecutionStates resets all execution states (useful for testing)
func (dr *DependencyResolver) ResetExecutionStates() {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dr.executions = make(map[string]*ExecutionState)

	if dr.logger != nil {
		dr.logger.Info("execution states reset")
	}
}

// HealthCheck performs a health check on the dependency resolver
func (dr *DependencyResolver) HealthCheck(ctx context.Context) error {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	// Check for circular dependencies
	if err := dr.graph.ValidateNoCycles(); err != nil {
		return common.ErrHealthCheckFailed("dependency_resolver", err)
	}

	// Check for too many waiting jobs
	stats := dr.GetDependencyStats()
	if stats.WaitingJobs > stats.TotalJobs/2 {
		return common.ErrHealthCheckFailed("dependency_resolver", fmt.Errorf("too many waiting jobs: %d", stats.WaitingJobs))
	}

	return nil
}
