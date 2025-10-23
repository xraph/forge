package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
)

// JobDefinition defines a cron job with its configuration
type JobDefinition struct {
	ID           string                 `json:"id" yaml:"id"`
	Name         string                 `json:"name" yaml:"name"`
	Schedule     string                 `json:"schedule" yaml:"schedule"` // Cron expression
	Handler      JobHandler             `json:"-" yaml:"-"`               // Handler function
	Config       map[string]interface{} `json:"config" yaml:"config"`
	Enabled      bool                   `json:"enabled" yaml:"enabled"`
	MaxRetries   int                    `json:"max_retries" yaml:"max_retries"`
	Timeout      time.Duration          `json:"timeout" yaml:"timeout"`
	Singleton    bool                   `json:"singleton" yaml:"singleton"`     // Only one instance across cluster
	Distributed  bool                   `json:"distributed" yaml:"distributed"` // Can run on multiple nodes
	Tags         map[string]string      `json:"tags" yaml:"tags"`
	Dependencies []string               `json:"dependencies" yaml:"dependencies"` // Job dependencies
	Priority     int                    `json:"priority" yaml:"priority"`         // Higher number = higher priority
	CreatedAt    time.Time              `json:"created_at" yaml:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at" yaml:"updated_at"`
}

// Validate validates the job definition
func (jd *JobDefinition) Validate() error {
	if jd.ID == "" {
		return common.ErrValidationError("id", fmt.Errorf("job ID cannot be empty"))
	}

	if jd.Name == "" {
		return common.ErrValidationError("name", fmt.Errorf("job name cannot be empty"))
	}

	if jd.Schedule == "" {
		return common.ErrValidationError("schedule", fmt.Errorf("job schedule cannot be empty"))
	}

	if jd.Handler == nil {
		return common.ErrValidationError("handler", fmt.Errorf("job handler cannot be nil"))
	}

	if jd.MaxRetries < 0 {
		return common.ErrValidationError("max_retries", fmt.Errorf("max retries cannot be negative"))
	}

	if jd.Timeout < 0 {
		return common.ErrValidationError("timeout", fmt.Errorf("timeout cannot be negative"))
	}

	if jd.Priority < 0 {
		return common.ErrValidationError("priority", fmt.Errorf("priority cannot be negative"))
	}

	// Validate cron expression
	if err := validateCronExpression(jd.Schedule); err != nil {
		return common.ErrValidationError("schedule", err)
	}

	return nil
}

// Job represents a cron job instance
type Job struct {
	Definition       *JobDefinition `json:"definition"`
	Status           JobStatus      `json:"status"`
	NextRun          time.Time      `json:"next_run"`
	LastRun          time.Time      `json:"last_run"`
	RunCount         int64          `json:"run_count"`
	FailureCount     int64          `json:"failure_count"`
	SuccessCount     int64          `json:"success_count"`
	LeaderNode       string         `json:"leader_node"`
	AssignedNode     string         `json:"assigned_node"`
	CurrentExecution *JobExecution  `json:"current_execution,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// JobStatus represents the status of a job
type JobStatus string

const (
	JobStatusActive    JobStatus = "active"
	JobStatusDisabled  JobStatus = "disabled"
	JobStatusRunning   JobStatus = "running"
	JobStatusFailed    JobStatus = "failed"
	JobStatusPaused    JobStatus = "paused"
	JobStatusCompleted JobStatus = "completed"
)

// IsRunnable returns true if the job can be executed
func (j *Job) IsRunnable() bool {
	return j.Status == JobStatusActive && j.Definition.Enabled
}

// CanRetry returns true if the job can be retried
func (j *Job) CanRetry() bool {
	return j.FailureCount < int64(j.Definition.MaxRetries)
}

// GetSuccessRate returns the success rate of the job
func (j *Job) GetSuccessRate() float64 {
	if j.RunCount == 0 {
		return 0.0
	}
	return float64(j.SuccessCount) / float64(j.RunCount)
}

// GetFailureRate returns the failure rate of the job
func (j *Job) GetFailureRate() float64 {
	if j.RunCount == 0 {
		return 0.0
	}
	return float64(j.FailureCount) / float64(j.RunCount)
}

// UpdateStatus updates the job status
func (j *Job) UpdateStatus(status JobStatus) {
	j.Status = status
	j.UpdatedAt = time.Now()
}

// IncrementRunCount increments the run count
func (j *Job) IncrementRunCount() {
	j.RunCount++
	j.UpdatedAt = time.Now()
}

// IncrementSuccessCount increments the success count
func (j *Job) IncrementSuccessCount() {
	j.SuccessCount++
	j.UpdatedAt = time.Now()
}

// IncrementFailureCount increments the failure count
func (j *Job) IncrementFailureCount() {
	j.FailureCount++
	j.UpdatedAt = time.Now()
}

// JobExecution represents a single execution of a job
type JobExecution struct {
	ID        string                 `json:"id"`
	JobID     string                 `json:"job_id"`
	NodeID    string                 `json:"node_id"`
	Status    ExecutionStatus        `json:"status"`
	StartTime time.Time              `json:"start_time"`
	EndTime   time.Time              `json:"end_time"`
	Duration  time.Duration          `json:"duration"`
	Output    string                 `json:"output"`
	Error     string                 `json:"error,omitempty"`
	Attempt   int                    `json:"attempt"`
	Metadata  map[string]interface{} `json:"metadata"`
	Tags      map[string]string      `json:"tags"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// ExecutionStatus represents the status of a job execution
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusSuccess   ExecutionStatus = "success"
	ExecutionStatusFailed    ExecutionStatus = "failed"
	ExecutionStatusCancelled ExecutionStatus = "cancelled"
	ExecutionStatusTimeout   ExecutionStatus = "timeout"
	ExecutionStatusRetrying  ExecutionStatus = "retrying"
)

// IsComplete returns true if the execution is complete
func (je *JobExecution) IsComplete() bool {
	return je.Status == ExecutionStatusSuccess ||
		je.Status == ExecutionStatusFailed ||
		je.Status == ExecutionStatusCancelled ||
		je.Status == ExecutionStatusTimeout
}

// IsSuccess returns true if the execution was successful
func (je *JobExecution) IsSuccess() bool {
	return je.Status == ExecutionStatusSuccess
}

// IsFailure returns true if the execution failed
func (je *JobExecution) IsFailure() bool {
	return je.Status == ExecutionStatusFailed ||
		je.Status == ExecutionStatusTimeout
}

// MarkStarted marks the execution as started
func (je *JobExecution) MarkStarted() {
	je.Status = ExecutionStatusRunning
	je.StartTime = time.Now()
	je.UpdatedAt = time.Now()
}

// MarkCompleted marks the execution as completed
func (je *JobExecution) MarkCompleted(status ExecutionStatus, output string, err error) {
	je.Status = status
	je.EndTime = time.Now()
	je.Duration = je.EndTime.Sub(je.StartTime)
	je.Output = output
	if err != nil {
		je.Error = err.Error()
	}
	je.UpdatedAt = time.Now()
}

// JobFilter represents filter criteria for querying jobs
type JobFilter struct {
	IDs           []string          `json:"ids,omitempty"`
	Names         []string          `json:"names,omitempty"`
	Statuses      []JobStatus       `json:"statuses,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	Enabled       *bool             `json:"enabled,omitempty"`
	Singleton     *bool             `json:"singleton,omitempty"`
	Distributed   *bool             `json:"distributed,omitempty"`
	CreatedAfter  *time.Time        `json:"created_after,omitempty"`
	CreatedBefore *time.Time        `json:"created_before,omitempty"`
	UpdatedAfter  *time.Time        `json:"updated_after,omitempty"`
	UpdatedBefore *time.Time        `json:"updated_before,omitempty"`
	Limit         int               `json:"limit,omitempty"`
	Offset        int               `json:"offset,omitempty"`
	SortBy        string            `json:"sort_by,omitempty"`
	SortOrder     string            `json:"sort_order,omitempty"`
}

// ExecutionFilter represents filter criteria for querying job executions
type ExecutionFilter struct {
	IDs           []string          `json:"ids,omitempty"`
	JobIDs        []string          `json:"job_ids,omitempty"`
	NodeIDs       []string          `json:"node_ids,omitempty"`
	Statuses      []ExecutionStatus `json:"statuses,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	StartedAfter  *time.Time        `json:"started_after,omitempty"`
	StartedBefore *time.Time        `json:"started_before,omitempty"`
	EndedAfter    *time.Time        `json:"ended_after,omitempty"`
	EndedBefore   *time.Time        `json:"ended_before,omitempty"`
	Limit         int               `json:"limit,omitempty"`
	Offset        int               `json:"offset,omitempty"`
	SortBy        string            `json:"sort_by,omitempty"`
	SortOrder     string            `json:"sort_order,omitempty"`
}

// JobStats represents statistics for a job
type JobStats struct {
	JobID           string        `json:"job_id"`
	TotalExecutions int64         `json:"total_executions"`
	SuccessfulRuns  int64         `json:"successful_runs"`
	FailedRuns      int64         `json:"failed_runs"`
	SuccessRate     float64       `json:"success_rate"`
	FailureRate     float64       `json:"failure_rate"`
	AverageRunTime  time.Duration `json:"average_run_time"`
	MinRunTime      time.Duration `json:"min_run_time"`
	MaxRunTime      time.Duration `json:"max_run_time"`
	LastRunTime     time.Time     `json:"last_run_time"`
	NextRunTime     time.Time     `json:"next_run_time"`
	CurrentStatus   JobStatus     `json:"current_status"`
	AssignedNode    string        `json:"assigned_node"`
	LastError       string        `json:"last_error,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

// CronStats represents overall statistics for the cron system
type CronStats struct {
	TotalJobs            int64         `json:"total_jobs"`
	ActiveJobs           int64         `json:"active_jobs"`
	RunningJobs          int64         `json:"running_jobs"`
	FailedJobs           int64         `json:"failed_jobs"`
	DisabledJobs         int64         `json:"disabled_jobs"`
	TotalExecutions      int64         `json:"total_executions"`
	SuccessfulExecutions int64         `json:"successful_executions"`
	FailedExecutions     int64         `json:"failed_executions"`
	SuccessRate          float64       `json:"success_rate"`
	FailureRate          float64       `json:"failure_rate"`
	AverageJobDuration   time.Duration `json:"average_job_duration"`
	ActiveNodes          int           `json:"active_nodes"`
	LeaderNode           string        `json:"leader_node"`
	ClusterHealthy       bool          `json:"cluster_healthy"`
	LastUpdate           time.Time     `json:"last_update"`
}

// JobEvent represents an event in the job lifecycle
type JobEvent struct {
	Type      JobEventType           `json:"type"`
	JobID     string                 `json:"job_id"`
	NodeID    string                 `json:"node_id"`
	Status    interface{}            `json:"status"` // JobStatus or ExecutionStatus
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata"`
	Error     string                 `json:"error,omitempty"`
}

// JobEventType represents the type of job event
type JobEventType string

const (
	JobEventTypeCreated    JobEventType = "job.created"
	JobEventTypeUpdated    JobEventType = "job.updated"
	JobEventTypeDeleted    JobEventType = "job.deleted"
	JobEventTypeScheduled  JobEventType = "job.scheduled"
	JobEventTypeStarted    JobEventType = "job.started"
	JobEventTypeCompleted  JobEventType = "job.completed"
	JobEventTypeFailed     JobEventType = "job.failed"
	JobEventTypeRetrying   JobEventType = "job.retrying"
	JobEventTypeCancelled  JobEventType = "job.cancelled"
	JobEventTypeTimeout    JobEventType = "job.timeout"
	JobEventTypeEnabled    JobEventType = "job.enabled"
	JobEventTypeDisabled   JobEventType = "job.disabled"
	JobEventTypeAssigned   JobEventType = "job.assigned"
	JobEventTypeReassigned JobEventType = "job.reassigned"
)

// NewJob creates a new job from a definition
func NewJob(definition *JobDefinition) (*Job, error) {
	if err := definition.Validate(); err != nil {
		return nil, err
	}

	now := time.Now()
	nextRun, err := getNextRunTime(definition.Schedule, now)
	if err != nil {
		return nil, common.ErrInvalidConfig("schedule", err)
	}

	return &Job{
		Definition:   definition,
		Status:       JobStatusActive,
		NextRun:      nextRun,
		RunCount:     0,
		FailureCount: 0,
		SuccessCount: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// NewJobExecution creates a new job execution
func NewJobExecution(jobID, nodeID string, attempt int) *JobExecution {
	now := time.Now()
	return &JobExecution{
		ID:        generateExecutionID(jobID, nodeID, now),
		JobID:     jobID,
		NodeID:    nodeID,
		Status:    ExecutionStatusPending,
		Attempt:   attempt,
		Metadata:  make(map[string]interface{}),
		Tags:      make(map[string]string),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// generateExecutionID generates a unique execution ID
func generateExecutionID(jobID, nodeID string, timestamp time.Time) string {
	return fmt.Sprintf("%s-%s-%d", jobID, nodeID, timestamp.UnixNano())
}

// validateCronExpression validates a cron expression
func validateCronExpression(expr string) error {
	// Basic validation - in a real implementation, use a proper cron parser
	parts := strings.Fields(expr)
	if len(parts) < 5 || len(parts) > 6 {
		return fmt.Errorf("invalid cron expression: %s", expr)
	}
	return nil
}

// getNextRunTime calculates the next run time based on cron expression
func getNextRunTime(schedule string, from time.Time) (time.Time, error) {
	// Simplified implementation - in real code, use a proper cron parser
	// For now, return a time 1 minute from now
	return from.Add(time.Minute), nil
}
