package cron

import (
	"context"
	"time"
)

// Job represents a scheduled job.
type Job struct {
	// ID is the unique identifier for this job
	ID string `json:"id" bson:"_id,omitempty"`

	// Name is a human-readable name for the job
	Name string `json:"name" bson:"name"`

	// Schedule is the cron expression defining when the job runs
	Schedule string `json:"schedule" bson:"schedule"`

	// Handler is the function to execute (for code-based jobs)
	// This field is not persisted to storage
	Handler JobHandler `json:"-" bson:"-"`

	// HandlerName is the name of the registered handler (for code-based jobs)
	HandlerName string `json:"handlerName,omitempty" bson:"handler_name,omitempty"`

	// Command is the shell command to execute (for command-based jobs)
	Command string `json:"command,omitempty" bson:"command,omitempty"`

	// Args are arguments for the command
	Args []string `json:"args,omitempty" bson:"args,omitempty"`

	// Env are environment variables for the command
	Env []string `json:"env,omitempty" bson:"env,omitempty"`

	// WorkingDir is the working directory for command execution
	WorkingDir string `json:"workingDir,omitempty" bson:"working_dir,omitempty"`

	// Payload is arbitrary data passed to the job handler
	Payload map[string]interface{} `json:"payload,omitempty" bson:"payload,omitempty"`

	// Enabled indicates whether the job is active
	Enabled bool `json:"enabled" bson:"enabled"`

	// Timezone specifies the timezone for schedule evaluation
	Timezone *time.Location `json:"timezone,omitempty" bson:"timezone,omitempty"`

	// MaxRetries is the maximum number of retry attempts on failure
	MaxRetries int `json:"maxRetries" bson:"max_retries"`

	// Timeout is the maximum execution time for the job
	Timeout time.Duration `json:"timeout" bson:"timeout"`

	// Metadata contains arbitrary key-value metadata
	Metadata map[string]string `json:"metadata,omitempty" bson:"metadata,omitempty"`

	// Tags for categorizing jobs
	Tags []string `json:"tags,omitempty" bson:"tags,omitempty"`

	// CreatedAt is when the job was created
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`

	// UpdatedAt is when the job was last updated
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`

	// LastExecutionAt is when the job last executed
	LastExecutionAt *time.Time `json:"lastExecutionAt,omitempty" bson:"last_execution_at,omitempty"`

	// NextExecutionAt is when the job will next execute
	NextExecutionAt *time.Time `json:"nextExecutionAt,omitempty" bson:"next_execution_at,omitempty"`
}

// JobHandler is the function signature for code-based jobs.
// The handler receives a context (for cancellation) and the job definition.
// Return an error if the job fails.
type JobHandler func(ctx context.Context, job *Job) error

// ExecutionStatus represents the status of a job execution.
type ExecutionStatus string

const (
	// ExecutionStatusPending indicates the execution is queued but not started
	ExecutionStatusPending ExecutionStatus = "pending"

	// ExecutionStatusRunning indicates the execution is in progress
	ExecutionStatusRunning ExecutionStatus = "running"

	// ExecutionStatusSuccess indicates the execution completed successfully
	ExecutionStatusSuccess ExecutionStatus = "success"

	// ExecutionStatusFailed indicates the execution failed
	ExecutionStatusFailed ExecutionStatus = "failed"

	// ExecutionStatusCancelled indicates the execution was cancelled
	ExecutionStatusCancelled ExecutionStatus = "cancelled"

	// ExecutionStatusTimeout indicates the execution exceeded the timeout
	ExecutionStatusTimeout ExecutionStatus = "timeout"

	// ExecutionStatusRetrying indicates the execution is being retried
	ExecutionStatusRetrying ExecutionStatus = "retrying"
)

// JobExecution represents a single execution of a job.
type JobExecution struct {
	// ID is the unique identifier for this execution
	ID string `json:"id" bson:"_id,omitempty"`

	// JobID is the ID of the job being executed
	JobID string `json:"jobId" bson:"job_id"`

	// JobName is a denormalized copy of the job name for convenience
	JobName string `json:"jobName" bson:"job_name"`

	// Status is the current status of the execution
	Status ExecutionStatus `json:"status" bson:"status"`

	// ScheduledAt is when the job was scheduled to run
	ScheduledAt time.Time `json:"scheduledAt" bson:"scheduled_at"`

	// StartedAt is when the execution actually started
	StartedAt time.Time `json:"startedAt" bson:"started_at"`

	// CompletedAt is when the execution finished (success or failure)
	CompletedAt *time.Time `json:"completedAt,omitempty" bson:"completed_at,omitempty"`

	// Error contains the error message if the execution failed
	Error string `json:"error,omitempty" bson:"error,omitempty"`

	// Output contains stdout/stderr from command execution
	Output string `json:"output,omitempty" bson:"output,omitempty"`

	// Retries is the number of retries attempted
	Retries int `json:"retries" bson:"retries"`

	// NodeID identifies which node executed the job (for distributed mode)
	NodeID string `json:"nodeId,omitempty" bson:"node_id,omitempty"`

	// Duration is how long the execution took
	Duration time.Duration `json:"duration" bson:"duration"`

	// Metadata contains arbitrary execution metadata
	Metadata map[string]string `json:"metadata,omitempty" bson:"metadata,omitempty"`
}

// JobStats contains aggregated statistics for a job.
type JobStats struct {
	JobID            string        `json:"jobId"`
	JobName          string        `json:"jobName"`
	TotalExecutions  int64         `json:"totalExecutions"`
	SuccessCount     int64         `json:"successCount"`
	FailureCount     int64         `json:"failureCount"`
	CancelCount      int64         `json:"cancelCount"`
	TimeoutCount     int64         `json:"timeoutCount"`
	AverageDuration  time.Duration `json:"averageDuration"`
	MinDuration      time.Duration `json:"minDuration"`
	MaxDuration      time.Duration `json:"maxDuration"`
	LastSuccess      *time.Time    `json:"lastSuccess,omitempty"`
	LastFailure      *time.Time    `json:"lastFailure,omitempty"`
	LastExecution    *time.Time    `json:"lastExecution,omitempty"`
	NextExecution    *time.Time    `json:"nextExecution,omitempty"`
	SuccessRate      float64       `json:"successRate"` // Percentage
	RecentExecutions int           `json:"recentExecutions"`
}

// SchedulerStats contains overall scheduler statistics.
type SchedulerStats struct {
	TotalJobs         int            `json:"totalJobs"`
	EnabledJobs       int            `json:"enabledJobs"`
	DisabledJobs      int            `json:"disabledJobs"`
	RunningExecutions int            `json:"runningExecutions"`
	QueuedExecutions  int            `json:"queuedExecutions"`
	IsLeader          bool           `json:"isLeader"`
	NodeID            string         `json:"nodeId"`
	Uptime            time.Duration  `json:"uptime"`
	JobStats          map[string]int `json:"jobStats"` // Status counts
}

// JobUpdate represents fields that can be updated on a job.
type JobUpdate struct {
	Name        *string                `json:"name,omitempty"`
	Schedule    *string                `json:"schedule,omitempty"`
	HandlerName *string                `json:"handlerName,omitempty"`
	Command     *string                `json:"command,omitempty"`
	Args        []string               `json:"args,omitempty"`
	Env         []string               `json:"env,omitempty"`
	WorkingDir  *string                `json:"workingDir,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	Enabled     *bool                  `json:"enabled,omitempty"`
	MaxRetries  *int                   `json:"maxRetries,omitempty"`
	Timeout     *time.Duration         `json:"timeout,omitempty"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
}

// ExecutionFilter is used to filter job executions when querying.
type ExecutionFilter struct {
	JobID      string            `json:"jobId,omitempty"`
	Status     []ExecutionStatus `json:"status,omitempty"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	Before     *time.Time        `json:"before,omitempty"`
	After      *time.Time        `json:"after,omitempty"`
	NodeID     string            `json:"nodeId,omitempty"`
	Limit      int               `json:"limit,omitempty"`
	Offset     int               `json:"offset,omitempty"`
	OrderBy    string            `json:"orderBy,omitempty"`    // Field to order by
	OrderDir   string            `json:"orderDir,omitempty"`   // "asc" or "desc"
}

