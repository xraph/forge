package cron

import (
	"context"
	"time"
)

// Job represents a scheduled job.
type Job struct {
	// ID is the unique identifier for this job
	ID string `bson:"_id,omitempty" json:"id"`

	// Name is a human-readable name for the job
	Name string `bson:"name" json:"name"`

	// Schedule is the cron expression defining when the job runs
	Schedule string `bson:"schedule" json:"schedule"`

	// Handler is the function to execute (for code-based jobs)
	// This field is not persisted to storage
	Handler JobHandler `bson:"-" json:"-"`

	// HandlerName is the name of the registered handler (for code-based jobs)
	HandlerName string `bson:"handler_name,omitempty" json:"handlerName,omitempty"`

	// Command is the shell command to execute (for command-based jobs)
	Command string `bson:"command,omitempty" json:"command,omitempty"`

	// Args are arguments for the command
	Args []string `bson:"args,omitempty" json:"args,omitempty"`

	// Env are environment variables for the command
	Env []string `bson:"env,omitempty" json:"env,omitempty"`

	// WorkingDir is the working directory for command execution
	WorkingDir string `bson:"working_dir,omitempty" json:"workingDir,omitempty"`

	// Payload is arbitrary data passed to the job handler
	Payload map[string]any `bson:"payload,omitempty" json:"payload,omitempty"`

	// Enabled indicates whether the job is active
	Enabled bool `bson:"enabled" json:"enabled"`

	// Timezone specifies the timezone for schedule evaluation
	Timezone *time.Location `bson:"timezone,omitempty" json:"timezone,omitempty"`

	// MaxRetries is the maximum number of retry attempts on failure
	MaxRetries int `bson:"max_retries" json:"maxRetries"`

	// Timeout is the maximum execution time for the job
	Timeout time.Duration `bson:"timeout" json:"timeout"`

	// Metadata contains arbitrary key-value metadata
	Metadata map[string]string `bson:"metadata,omitempty" json:"metadata,omitempty"`

	// Tags for categorizing jobs
	Tags []string `bson:"tags,omitempty" json:"tags,omitempty"`

	// CreatedAt is when the job was created
	CreatedAt time.Time `bson:"created_at" json:"createdAt"`

	// UpdatedAt is when the job was last updated
	UpdatedAt time.Time `bson:"updated_at" json:"updatedAt"`

	// LastExecutionAt is when the job last executed
	LastExecutionAt *time.Time `bson:"last_execution_at,omitempty" json:"lastExecutionAt,omitempty"`

	// NextExecutionAt is when the job will next execute
	NextExecutionAt *time.Time `bson:"next_execution_at,omitempty" json:"nextExecutionAt,omitempty"`
}

// JobHandler is the function signature for code-based jobs.
// The handler receives a context (for cancellation) and the job definition.
// Return an error if the job fails.
type JobHandler func(ctx context.Context, job *Job) error

// ExecutionStatus represents the status of a job execution.
type ExecutionStatus string

const (
	// ExecutionStatusPending indicates the execution is queued but not started.
	ExecutionStatusPending ExecutionStatus = "pending"

	// ExecutionStatusRunning indicates the execution is in progress.
	ExecutionStatusRunning ExecutionStatus = "running"

	// ExecutionStatusSuccess indicates the execution completed successfully.
	ExecutionStatusSuccess ExecutionStatus = "success"

	// ExecutionStatusFailed indicates the execution failed.
	ExecutionStatusFailed ExecutionStatus = "failed"

	// ExecutionStatusCancelled indicates the execution was cancelled.
	ExecutionStatusCancelled ExecutionStatus = "cancelled"

	// ExecutionStatusTimeout indicates the execution exceeded the timeout.
	ExecutionStatusTimeout ExecutionStatus = "timeout"

	// ExecutionStatusRetrying indicates the execution is being retried.
	ExecutionStatusRetrying ExecutionStatus = "retrying"
)

// JobExecution represents a single execution of a job.
type JobExecution struct {
	// ID is the unique identifier for this execution
	ID string `bson:"_id,omitempty" json:"id"`

	// JobID is the ID of the job being executed
	JobID string `bson:"job_id" json:"jobId"`

	// JobName is a denormalized copy of the job name for convenience
	JobName string `bson:"job_name" json:"jobName"`

	// Status is the current status of the execution
	Status ExecutionStatus `bson:"status" json:"status"`

	// ScheduledAt is when the job was scheduled to run
	ScheduledAt time.Time `bson:"scheduled_at" json:"scheduledAt"`

	// StartedAt is when the execution actually started
	StartedAt time.Time `bson:"started_at" json:"startedAt"`

	// CompletedAt is when the execution finished (success or failure)
	CompletedAt *time.Time `bson:"completed_at,omitempty" json:"completedAt,omitempty"`

	// Error contains the error message if the execution failed
	Error string `bson:"error,omitempty" json:"error,omitempty"`

	// Output contains stdout/stderr from command execution
	Output string `bson:"output,omitempty" json:"output,omitempty"`

	// Retries is the number of retries attempted
	Retries int `bson:"retries" json:"retries"`

	// NodeID identifies which node executed the job (for distributed mode)
	NodeID string `bson:"node_id,omitempty" json:"nodeId,omitempty"`

	// Duration is how long the execution took
	Duration time.Duration `bson:"duration" json:"duration"`

	// Metadata contains arbitrary execution metadata
	Metadata map[string]string `bson:"metadata,omitempty" json:"metadata,omitempty"`
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
	Name        *string           `json:"name,omitempty"`
	Schedule    *string           `json:"schedule,omitempty"`
	HandlerName *string           `json:"handlerName,omitempty"`
	Command     *string           `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         []string          `json:"env,omitempty"`
	WorkingDir  *string           `json:"workingDir,omitempty"`
	Payload     map[string]any    `json:"payload,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	MaxRetries  *int              `json:"maxRetries,omitempty"`
	Timeout     *time.Duration    `json:"timeout,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
}

// ExecutionFilter is used to filter job executions when querying.
type ExecutionFilter struct {
	JobID     string            `json:"jobId,omitempty"`
	Status    []ExecutionStatus `json:"status,omitempty"`
	StartedAt *time.Time        `json:"startedAt,omitempty"`
	Before    *time.Time        `json:"before,omitempty"`
	After     *time.Time        `json:"after,omitempty"`
	NodeID    string            `json:"nodeId,omitempty"`
	Limit     int               `json:"limit,omitempty"`
	Offset    int               `json:"offset,omitempty"`
	OrderBy   string            `json:"orderBy,omitempty"`  // Field to order by
	OrderDir  string            `json:"orderDir,omitempty"` // "asc" or "desc"
}
