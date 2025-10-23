package cron

import (
	croncore "github.com/xraph/forge/pkg/cron/core"
)

// JobDefinition defines a cron job with its configuration
type JobDefinition = croncore.JobDefinition

// Job represents a cron job instance
type Job = croncore.Job

// JobStatus represents the status of a job
type JobStatus = croncore.JobStatus

const (
	JobStatusActive    = croncore.JobStatusActive
	JobStatusDisabled  = croncore.JobStatusDisabled
	JobStatusRunning   = croncore.JobStatusRunning
	JobStatusFailed    = croncore.JobStatusFailed
	JobStatusPaused    = croncore.JobStatusPaused
	JobStatusCompleted = croncore.JobStatusCompleted
)

// JobExecution represents a single execution of a job
type JobExecution = croncore.JobExecution

// ExecutionStatus represents the status of a job execution
type ExecutionStatus = croncore.ExecutionStatus

const (
	ExecutionStatusPending   = croncore.ExecutionStatusPending
	ExecutionStatusRunning   = croncore.ExecutionStatusRunning
	ExecutionStatusSuccess   = croncore.ExecutionStatusSuccess
	ExecutionStatusFailed    = croncore.ExecutionStatusFailed
	ExecutionStatusCancelled = croncore.ExecutionStatusCancelled
	ExecutionStatusTimeout   = croncore.ExecutionStatusTimeout
	ExecutionStatusRetrying  = croncore.ExecutionStatusRetrying
)

// JobFilter represents filter criteria for querying jobs
type JobFilter = croncore.JobFilter

// ExecutionFilter represents filter criteria for querying job executions
type ExecutionFilter = croncore.ExecutionFilter

// JobStats represents statistics for a job
type JobStats = croncore.JobStats

// CronStats represents overall statistics for the cron system
type CronStats = croncore.CronStats

// JobEvent represents an event in the job lifecycle
type JobEvent = croncore.JobEvent

// JobEventType represents the type of job event
type JobEventType = croncore.JobEventType

const (
	JobEventTypeCreated    = croncore.JobEventTypeCreated
	JobEventTypeUpdated    = croncore.JobEventTypeUpdated
	JobEventTypeDeleted    = croncore.JobEventTypeDeleted
	JobEventTypeScheduled  = croncore.JobEventTypeScheduled
	JobEventTypeStarted    = croncore.JobEventTypeStarted
	JobEventTypeCompleted  = croncore.JobEventTypeCompleted
	JobEventTypeFailed     = croncore.JobEventTypeFailed
	JobEventTypeRetrying   = croncore.JobEventTypeRetrying
	JobEventTypeCancelled  = croncore.JobEventTypeCancelled
	JobEventTypeTimeout    = croncore.JobEventTypeTimeout
	JobEventTypeEnabled    = croncore.JobEventTypeEnabled
	JobEventTypeDisabled   = croncore.JobEventTypeDisabled
	JobEventTypeAssigned   = croncore.JobEventTypeAssigned
	JobEventTypeReassigned = croncore.JobEventTypeReassigned
)

// NewJob creates a new job from a definition
func NewJob(definition *JobDefinition) (*Job, error) {
	return croncore.NewJob(definition)
}

// NewJobExecution creates a new job execution
func NewJobExecution(jobID, nodeID string, attempt int) *JobExecution {
	return croncore.NewJobExecution(jobID, nodeID, attempt)
}
