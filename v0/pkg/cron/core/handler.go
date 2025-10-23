package core

import (
	"context"
)

// JobHandler defines the interface for job execution handlers
type JobHandler interface {
	// Execute executes the job and returns the result
	Execute(ctx context.Context, job *Job) error

	// Validate validates the job configuration
	Validate(job *Job) error

	// OnSuccess is called when the job execution succeeds
	OnSuccess(ctx context.Context, job *Job, execution *JobExecution) error

	// OnFailure is called when the job execution fails
	OnFailure(ctx context.Context, job *Job, execution *JobExecution, err error) error

	// OnTimeout is called when the job execution times out
	OnTimeout(ctx context.Context, job *Job, execution *JobExecution) error

	// OnRetry is called when the job is being retried
	OnRetry(ctx context.Context, job *Job, execution *JobExecution, attempt int) error

	// GetMetadata returns metadata about the handler
	GetMetadata() map[string]interface{}
}
